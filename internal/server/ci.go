package server

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// ciState is the server's in-process CI state. Everything durable lives in
// the database, so several server nodes can share one queue; the notifiers
// only make this node react quickly (other nodes poll every few seconds).
type ciState struct {
	settings settingsCache
	// wake fires when a job may have become assignable (queued, a slot
	// freed, limits raised), waking long-polling runners.
	wake notifier
	// logs fires when job logs or job/step status changed, waking log
	// streams.
	logs notifier
	// assignMu serialises job assignment within this process.
	assignMu sync.Mutex
	// now is the clock (tests replace it).
	now func() time.Time
}

func (s *Server) now() time.Time {
	if s.ci.now != nil {
		return s.ci.now().UTC()
	}
	return time.Now().UTC()
}

// notifier is a broadcast signal: wait returns a channel closed by the next
// notify.
type notifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func (n *notifier) wait() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch == nil {
		n.ch = make(chan struct{})
	}
	return n.ch
}

func (n *notifier) notify() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch != nil {
		close(n.ch)
		n.ch = nil
	}
}

// jobDef is the resolved part of a job definition stored with each job.
type jobDef struct {
	Steps           []runnerapi.Step    `json:"steps"`
	Services        []runnerapi.Service `json:"services,omitempty"`
	ContainerEnv    map[string]string   `json:"container_env,omitempty"`
	WorkflowEnv     map[string]string   `json:"workflow_env,omitempty"`
	JobEnv          map[string]string   `json:"job_env,omitempty"`
	Defaults        workflow.Defaults   `json:"defaults"`
	Outputs         map[string]string   `json:"outputs,omitempty"`
	If              string              `json:"if,omitempty"`
	ContinueOnError bool                `json:"continue_on_error,omitempty"`
	FailFast        bool                `json:"fail_fast,omitempty"`
	Strategy        map[string]any      `json:"strategy,omitempty"`
	// Secrets lists the secret names the job references ("*" = all).
	Secrets []string `json:"secrets,omitempty"`
}

func parseDef(j *model.Job) jobDef {
	var d jobDef
	_ = json.Unmarshal([]byte(j.Definition), &d)
	return d
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func jsonStrings(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func jsonMap(s string) map[string]any {
	if s == "" {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func jsonStringMap(s string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

var (
	secretRefRe   = regexp.MustCompile(`(?i)\bsecrets\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)`)
	secretIndexRe = regexp.MustCompile(`(?i)\bsecrets\s*\[\s*'([^']+)'\s*\]`)
	secretBareRe  = regexp.MustCompile(`(?i)\bsecrets\b`)
)

// referencedSecrets finds the secret names used in expressions in strs;
// a bare use of the secrets context (e.g. toJSON(secrets)) means all.
func referencedSecrets(strs []string) []string {
	set := map[string]bool{}
	for _, s := range strs {
		for {
			i := strings.Index(s, "${{")
			if i < 0 {
				break
			}
			end := strings.Index(s[i:], "}}")
			if end < 0 {
				break
			}
			e := s[i+3 : i+end]
			s = s[i+end+2:]
			for _, m := range secretRefRe.FindAllStringSubmatch(e, -1) {
				set[strings.ToUpper(m[1])] = true
			}
			for _, m := range secretIndexRe.FindAllStringSubmatch(e, -1) {
				set[strings.ToUpper(m[1])] = true
			}
			stripped := secretRefRe.ReplaceAllString(secretIndexRe.ReplaceAllString(e, ""), "")
			if secretBareRe.MatchString(stripped) {
				return []string{"*"}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// jobStrings collects every string of a job that may hold expressions.
func jobStrings(w *workflow.Workflow, j *workflow.Job) []string {
	var out []string
	addMap := func(m map[string]string) {
		for _, v := range m {
			out = append(out, v)
		}
	}
	addMap(w.Env)
	addMap(j.Env)
	addMap(j.Outputs)
	if j.Container != nil {
		addMap(j.Container.Env)
	}
	for _, svc := range j.Services {
		addMap(svc.Env)
	}
	for _, st := range j.Steps {
		out = append(out, st.Run, st.If, st.Name, st.WorkingDirectory)
		addMap(st.With)
		addMap(st.Env)
	}
	return out
}

// conclusionOK reports whether a job's result counts as passing for its
// dependents and the run.
func conclusionOK(j *model.Job, d jobDef) bool {
	switch j.Conclusion {
	case model.ConclusionSuccess:
		return true
	case model.ConclusionFailure, model.ConclusionTimedOut:
		return d.ContinueOnError
	}
	return false
}

// needResult aggregates the instances of one job key into the result the
// needs context reports.
func needResult(insts []*model.Job, defs map[int64]jobDef) (string, map[string]any) {
	outputs := map[string]any{}
	failed, cancelled, skipped := false, false, 0
	for _, j := range insts {
		for k, v := range jsonStringMap(j.Outputs) {
			outputs[k] = v
		}
		if conclusionOK(j, defs[j.ID]) {
			continue
		}
		switch j.Conclusion {
		case model.ConclusionCancelled:
			cancelled = true
		case model.ConclusionSkipped:
			skipped++
		default:
			failed = true
		}
	}
	switch {
	case failed:
		return model.ConclusionFailure, outputs
	case cancelled:
		return model.ConclusionCancelled, outputs
	case skipped == len(insts):
		return model.ConclusionSkipped, outputs
	case skipped > 0:
		return model.ConclusionFailure, outputs
	}
	return model.ConclusionSuccess, outputs
}

// record writes an audit entry for the current API caller.
func (s *Server) record(ctx context.Context, db bun.IDB, action, targetType, target string, details map[string]any) error {
	e := audit.Entry{Action: action, TargetType: targetType, Target: target, Details: details}
	if p := principal(ctx); p != nil {
		e.Actor = p.User
	}
	if v, ok := ctx.Value(httpKey).(httpPair); ok {
		e.IP = clientIP(v.r)
	}
	return audit.Record(ctx, db, e)
}
