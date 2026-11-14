// Package workflow parses and validates CI workflow files
// (.tgp/workflows/*.yml), matches them against events and expands jobs
// (matrix strategies) into concrete job instances.
//
// The format follows GitHub Actions closely so existing knowledge carries
// over; differences are documented in docs/ci.md. Every job runs in a Docker
// container, steps are shell commands or one of tgp's built-in steps, and
// jobs can request CPU, memory and process limits under resources:.
package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/expr"
)

// Dir is where workflow files live in a repository.
const Dir = ".tgp/workflows"

// Limits on workflow size.
const (
	MaxJobs           = 100
	MaxMatrixJobs     = 256
	MaxStepsPerJob    = 200
	MaxFileBytes      = 512 << 10
	MaxServices       = 10
	MaxDispatchInputs = 25
)

// Workflow is a parsed workflow file.
type Workflow struct {
	Name        string            `json:"name"`
	On          Triggers          `json:"on"`
	Env         map[string]string `json:"env,omitempty"`
	Concurrency *Concurrency      `json:"concurrency,omitempty"`
	Defaults    Defaults          `json:"defaults"`
	Jobs        map[string]*Job   `json:"jobs"`
	// JobOrder lists job keys in file order.
	JobOrder []string `json:"job_order"`
}

// Triggers says which events start the workflow. A nil field means the
// event doesn't trigger it.
type Triggers struct {
	Push        *PushTrigger     `json:"push,omitempty"`
	PullRequest *PullTrigger     `json:"pull_request,omitempty"`
	Dispatch    *DispatchTrigger `json:"workflow_dispatch,omitempty"`
}

// PushTrigger filters push events.
type PushTrigger struct {
	Branches       []string `json:"branches,omitempty"`
	BranchesIgnore []string `json:"branches_ignore,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	TagsIgnore     []string `json:"tags_ignore,omitempty"`
	Paths          []string `json:"paths,omitempty"`
	PathsIgnore    []string `json:"paths_ignore,omitempty"`
}

// PullTrigger filters pull request events by base branch, changed paths
// and activity type.
type PullTrigger struct {
	Branches       []string `json:"branches,omitempty"`
	BranchesIgnore []string `json:"branches_ignore,omitempty"`
	Paths          []string `json:"paths,omitempty"`
	PathsIgnore    []string `json:"paths_ignore,omitempty"`
	Types          []string `json:"types,omitempty"`
}

// PullTypes are the supported pull_request activity types.
var PullTypes = []string{"opened", "synchronize", "reopened", "ready_for_review"}

// DefaultPullTypes trigger when types: isn't given.
var DefaultPullTypes = []string{"opened", "synchronize", "reopened"}

// DispatchTrigger allows manual runs with typed inputs.
type DispatchTrigger struct {
	Inputs     map[string]*Input `json:"inputs,omitempty"`
	InputOrder []string          `json:"input_order,omitempty"`
}

// Input is a workflow_dispatch input.
type Input struct {
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
	Type        string   `json:"type"` // string, boolean, number, choice
	Options     []string `json:"options,omitempty"`
}

// Concurrency limits a workflow (or job) to one running and one pending
// run per group.
type Concurrency struct {
	Group            string `json:"group"`
	CancelInProgress bool   `json:"cancel_in_progress"`
}

// Defaults for run steps.
type Defaults struct {
	Shell            string `json:"shell,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

// Container is the job's image or a service container.
type Container struct {
	Image string            `json:"image"`
	Env   map[string]string `json:"env,omitempty"`
}

// Resources are the container limits a job asks for. Zero means "use the
// instance default".
type Resources struct {
	CPUs        float64 `json:"cpus,omitempty"`
	MemoryBytes int64   `json:"memory_bytes,omitempty"`
	Pids        int     `json:"pids,omitempty"`
}

// Strategy is a job's matrix strategy.
type Strategy struct {
	// Matrix maps each dimension to its values, in file order (MatrixKeys).
	Matrix      map[string][]any `json:"matrix,omitempty"`
	MatrixKeys  []string         `json:"matrix_keys,omitempty"`
	Include     []map[string]any `json:"include,omitempty"`
	Exclude     []map[string]any `json:"exclude,omitempty"`
	FailFast    bool             `json:"fail_fast"`
	MaxParallel int              `json:"max_parallel,omitempty"`
}

// Job is a job as written in the file (expressions unevaluated).
type Job struct {
	Key             string               `json:"key"`
	Name            string               `json:"name,omitempty"`
	RunsOn          []string             `json:"runs_on,omitempty"`
	Container       *Container           `json:"container,omitempty"`
	Services        map[string]Container `json:"services,omitempty"`
	Needs           []string             `json:"needs,omitempty"`
	If              string               `json:"if,omitempty"`
	Strategy        *Strategy            `json:"strategy,omitempty"`
	Env             map[string]string    `json:"env,omitempty"`
	TimeoutMinutes  int                  `json:"timeout_minutes,omitempty"`
	ContinueOnError bool                 `json:"continue_on_error,omitempty"`
	Resources       Resources            `json:"resources"`
	Outputs         map[string]string    `json:"outputs,omitempty"`
	Defaults        Defaults             `json:"defaults"`
	Steps           []Step               `json:"steps"`
	Line            int                  `json:"-"`
}

// Step is one step of a job.
type Step struct {
	ID               string            `json:"id,omitempty"`
	Name             string            `json:"name,omitempty"`
	If               string            `json:"if,omitempty"`
	Run              string            `json:"run,omitempty"`
	Shell            string            `json:"shell,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	Uses             string            `json:"uses,omitempty"` // normalised built-in name
	With             map[string]string `json:"with,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	ContinueOnError  bool              `json:"continue_on_error,omitempty"`
	TimeoutMinutes   int               `json:"timeout_minutes,omitempty"`
}

// DisplayName is the step's name, or a name derived from what it does.
func (s *Step) DisplayName() string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Uses != "":
		return "Run " + s.Uses
	}
	line := strings.TrimSpace(s.Run)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if len(line) > 80 {
		line = line[:77] + "..."
	}
	return "Run " + line
}

// Builtin describes a built-in step (uses: name).
type Builtin struct {
	Name string
	// With lists the accepted inputs and whether each is required.
	With map[string]bool
}

// Builtins are the steps available to uses:. GitHub-style names such as
// actions/checkout@v4 are accepted as aliases.
var Builtins = map[string]Builtin{
	"checkout": {Name: "checkout", With: map[string]bool{"ref": false, "fetch-depth": false, "path": false}},
}

// builtinName normalises a uses: value: "checkout", "actions/checkout@v4"
// and "tgp/checkout" all mean the checkout built-in.
func builtinName(uses string) string {
	u := strings.TrimSpace(uses)
	if i := strings.IndexByte(u, '@'); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		owner := u[:i]
		if owner != "actions" && owner != "tgp" {
			return ""
		}
		u = u[i+1:]
	}
	if _, ok := Builtins[u]; ok {
		return u
	}
	return ""
}

// Error is a problem in a workflow file, with its position.
type Error struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"column,omitempty"`
	Msg  string `json:"message"`
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
	}
	return e.File + ": " + e.Msg
}

// Errors is a list of problems.
type Errors []*Error

func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

var (
	idRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	envRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	memRe   = regexp.MustCompile(`^(?i)([0-9]+(?:\.[0-9]+)?)\s*([kmgt]i?b?|b)$`)
	shellRe = regexp.MustCompile(`^(bash|sh|python|python3|pwsh|node)$`)
)

// contexts usable in each place (beyond literals).
var (
	ctxWorkflowEnv = []string{"github", "inputs", "vars"}
	ctxJob         = []string{"github", "inputs", "vars", "matrix", "needs", "strategy"}
	ctxJobIf       = []string{"github", "inputs", "vars", "needs"}
	ctxStep        = []string{"github", "inputs", "vars", "matrix", "needs", "strategy", "env", "secrets", "steps", "job", "runner"}
	ctxJobOutputs  = []string{"github", "inputs", "vars", "matrix", "needs", "strategy", "env", "secrets", "steps", "job", "runner"}
	ctxConcurrency = []string{"github", "inputs", "vars"}
)

// Parse parses and validates a workflow file. file is the path used in
// error messages.
func Parse(file string, data []byte) (*Workflow, error) {
	if len(data) > MaxFileBytes {
		return nil, Errors{{File: file, Msg: fmt.Sprintf("file is larger than %d KiB", MaxFileBytes>>10)}}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		line := 0
		if m := regexp.MustCompile(`^line (\d+): (.*)$`).FindStringSubmatch(msg); m != nil {
			line, _ = strconv.Atoi(m[1])
			msg = m[2]
		}
		return nil, Errors{{File: file, Line: line, Msg: "invalid YAML: " + msg}}
	}
	d := &decoder{file: file}
	if len(doc.Content) == 0 {
		return nil, Errors{{File: file, Msg: "the file is empty"}}
	}
	w := d.workflow(doc.Content[0])
	if len(d.errs) > 0 {
		return nil, d.errs
	}
	return w, nil
}

type decoder struct {
	file string
	errs Errors
}

func (d *decoder) errorf(n *yaml.Node, format string, args ...any) {
	e := &Error{File: d.file, Msg: fmt.Sprintf(format, args...)}
	if n != nil {
		e.Line, e.Col = n.Line, n.Column
	}
	d.errs = append(d.errs, e)
}

// fields iterates a mapping, reporting keys not in allowed.
func (d *decoder) fields(n *yaml.Node, what string, allowed []string, f func(key string, k, v *yaml.Node)) {
	if n.Kind != yaml.MappingNode {
		d.errorf(n, "%s must be a mapping", what)
		return
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		key := k.Value
		if seen[key] {
			d.errorf(k, "%s: duplicate key %q", what, key)
			continue
		}
		seen[key] = true
		if allowed != nil && !contains(allowed, key) {
			d.errorf(k, "%s: unknown key %q (allowed: %s)", what, key, strings.Join(allowed, ", "))
			continue
		}
		f(key, k, v)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (d *decoder) str(n *yaml.Node, what string) string {
	if n.Kind != yaml.ScalarNode {
		d.errorf(n, "%s must be a string", what)
		return ""
	}
	if n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

func (d *decoder) strList(n *yaml.Node, what string) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				d.errorf(c, "%s must be a list of strings", what)
				continue
			}
			out = append(out, c.Value)
		}
		return out
	}
	d.errorf(n, "%s must be a string or a list of strings", what)
	return nil
}

func (d *decoder) strMap(n *yaml.Node, what string, keyRe *regexp.Regexp) map[string]string {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	out := map[string]string{}
	d.fields(n, what, nil, func(key string, k, v *yaml.Node) {
		if keyRe != nil && !keyRe.MatchString(key) {
			d.errorf(k, "%s: invalid name %q", what, key)
			return
		}
		if v.Kind != yaml.ScalarNode {
			d.errorf(v, "%s.%s must be a string, number or boolean", what, key)
			return
		}
		if v.Tag == "!!null" {
			out[key] = ""
			return
		}
		out[key] = v.Value
	})
	return out
}

func (d *decoder) boolean(n *yaml.Node, what string) bool {
	if n.Kind == yaml.ScalarNode {
		switch n.Value {
		case "true":
			return true
		case "false":
			return false
		}
	}
	d.errorf(n, "%s must be true or false", what)
	return false
}

func (d *decoder) integer(n *yaml.Node, what string, min, max int) int {
	if n.Kind == yaml.ScalarNode {
		if v, err := strconv.Atoi(n.Value); err == nil {
			if v < min || v > max {
				d.errorf(n, "%s must be between %d and %d", what, min, max)
			}
			return v
		}
	}
	d.errorf(n, "%s must be a whole number", what)
	return 0
}

// checkExpr validates the ${{ }} expressions in s and that they only use
// the allowed contexts.
func (d *decoder) checkExpr(n *yaml.Node, what, s string, allowed []string) {
	names, err := expr.Check(s)
	if err != nil {
		d.errorf(n, "%s: %v", what, err)
		return
	}
	d.checkNames(n, what, names, allowed)
}

func (d *decoder) checkNames(n *yaml.Node, what string, names, allowed []string) {
	for _, name := range names {
		if !contains(allowed, name) {
			d.errorf(n, "%s: the %q context isn't available here (available: %s)", what, name, strings.Join(allowed, ", "))
		}
	}
}

func (d *decoder) checkCond(n *yaml.Node, what, cond string, allowed []string) {
	c := strings.TrimSpace(cond)
	if strings.HasPrefix(c, "${{") && strings.HasSuffix(c, "}}") && strings.Count(c, "${{") == 1 {
		c = strings.TrimSpace(c[3 : len(c)-2])
	}
	if c == "" {
		return
	}
	e, err := expr.Parse(c)
	if err != nil {
		d.errorf(n, "%s: %v", what, err)
		return
	}
	d.checkNames(n, what, e.Contexts(), allowed)
}

func (d *decoder) workflow(n *yaml.Node) *Workflow {
	w := &Workflow{Jobs: map[string]*Job{}}
	var onNode, jobsNode *yaml.Node
	d.fields(n, "workflow", []string{"name", "on", "env", "concurrency", "defaults", "jobs", "permissions"}, func(key string, _, v *yaml.Node) {
		switch key {
		case "name":
			w.Name = d.str(v, "name")
		case "on":
			onNode = v
			w.On = d.triggers(v)
		case "env":
			w.Env = d.strMap(v, "env", envRe)
			for k, val := range w.Env {
				d.checkExpr(v, "env."+k, val, ctxWorkflowEnv)
			}
		case "concurrency":
			w.Concurrency = d.concurrency(v)
		case "defaults":
			w.Defaults = d.defaults(v)
		case "jobs":
			jobsNode = v
		case "permissions":
			// Accepted for compatibility with GitHub workflows; tgp job tokens
			// are always read-only for the repository.
		}
	})
	if onNode == nil {
		d.errorf(n, "missing \"on\": say which events run this workflow (push, pull_request, workflow_dispatch)")
	}
	if jobsNode == nil {
		d.errorf(n, "missing \"jobs\"")
		return w
	}
	d.fields(jobsNode, "jobs", nil, func(key string, k, v *yaml.Node) {
		if !idRe.MatchString(key) {
			d.errorf(k, "job id %q must start with a letter or _ and contain only letters, digits, - and _", key)
			return
		}
		j := d.job(key, v)
		j.Line = k.Line
		w.Jobs[key] = j
		w.JobOrder = append(w.JobOrder, key)
	})
	switch {
	case len(w.Jobs) == 0 && jobsNode.Kind == yaml.MappingNode:
		d.errorf(jobsNode, "jobs: the workflow has no jobs")
	case len(w.Jobs) > MaxJobs:
		d.errorf(jobsNode, "jobs: at most %d jobs per workflow", MaxJobs)
	}
	d.checkNeeds(w, jobsNode)
	return w
}

func (d *decoder) triggers(n *yaml.Node) Triggers {
	var t Triggers
	set := func(name string, v *yaml.Node, k *yaml.Node) {
		switch name {
		case "push":
			t.Push = &PushTrigger{}
			if v != nil && !(v.Kind == yaml.ScalarNode && v.Tag == "!!null") {
				d.fields(v, "on.push", []string{"branches", "branches-ignore", "tags", "tags-ignore", "paths", "paths-ignore"}, func(key string, _, v *yaml.Node) {
					l := d.patterns(v, "on.push."+key)
					switch key {
					case "branches":
						t.Push.Branches = l
					case "branches-ignore":
						t.Push.BranchesIgnore = l
					case "tags":
						t.Push.Tags = l
					case "tags-ignore":
						t.Push.TagsIgnore = l
					case "paths":
						t.Push.Paths = l
					case "paths-ignore":
						t.Push.PathsIgnore = l
					}
				})
				if t.Push.Branches != nil && t.Push.BranchesIgnore != nil {
					d.errorf(v, "on.push: use either branches or branches-ignore, not both")
				}
				if t.Push.Tags != nil && t.Push.TagsIgnore != nil {
					d.errorf(v, "on.push: use either tags or tags-ignore, not both")
				}
				if t.Push.Paths != nil && t.Push.PathsIgnore != nil {
					d.errorf(v, "on.push: use either paths or paths-ignore, not both")
				}
			}
		case "pull_request":
			t.PullRequest = &PullTrigger{}
			if v != nil && !(v.Kind == yaml.ScalarNode && v.Tag == "!!null") {
				d.fields(v, "on.pull_request", []string{"branches", "branches-ignore", "paths", "paths-ignore", "types"}, func(key string, _, v *yaml.Node) {
					switch key {
					case "types":
						t.PullRequest.Types = d.strList(v, "on.pull_request.types")
						for _, ty := range t.PullRequest.Types {
							if !contains(PullTypes, ty) {
								d.errorf(v, "on.pull_request.types: unknown type %q (supported: %s)", ty, strings.Join(PullTypes, ", "))
							}
						}
						return
					}
					l := d.patterns(v, "on.pull_request."+key)
					switch key {
					case "branches":
						t.PullRequest.Branches = l
					case "branches-ignore":
						t.PullRequest.BranchesIgnore = l
					case "paths":
						t.PullRequest.Paths = l
					case "paths-ignore":
						t.PullRequest.PathsIgnore = l
					}
				})
				if t.PullRequest.Branches != nil && t.PullRequest.BranchesIgnore != nil {
					d.errorf(v, "on.pull_request: use either branches or branches-ignore, not both")
				}
				if t.PullRequest.Paths != nil && t.PullRequest.PathsIgnore != nil {
					d.errorf(v, "on.pull_request: use either paths or paths-ignore, not both")
				}
			}
		case "workflow_dispatch":
			t.Dispatch = d.dispatch(v)
		case "schedule", "pull_request_target", "release", "issues", "workflow_run", "workflow_call":
			d.errorf(k, "on: the %q event isn't supported (supported: push, pull_request, workflow_dispatch)", name)
		default:
			d.errorf(k, "on: unknown event %q (supported: push, pull_request, workflow_dispatch)", name)
		}
	}
	switch n.Kind {
	case yaml.ScalarNode:
		set(n.Value, nil, n)
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				d.errorf(c, "on: list entries must be event names")
				continue
			}
			set(c.Value, nil, c)
		}
	case yaml.MappingNode:
		d.fields(n, "on", nil, func(key string, k, v *yaml.Node) { set(key, v, k) })
	default:
		d.errorf(n, "on must be an event name, a list or a mapping")
	}
	return t
}

func (d *decoder) patterns(n *yaml.Node, what string) []string {
	l := d.strList(n, what)
	if l == nil {
		l = []string{}
	}
	for _, p := range l {
		if _, err := globRegexp(strings.TrimPrefix(p, "!")); err != nil {
			d.errorf(n, "%s: invalid pattern %q: %v", what, p, err)
		}
	}
	return l
}

func (d *decoder) dispatch(n *yaml.Node) *DispatchTrigger {
	t := &DispatchTrigger{Inputs: map[string]*Input{}}
	if n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
		return t
	}
	d.fields(n, "on.workflow_dispatch", []string{"inputs"}, func(_ string, _, v *yaml.Node) {
		d.fields(v, "on.workflow_dispatch.inputs", nil, func(name string, k, v *yaml.Node) {
			if !idRe.MatchString(name) {
				d.errorf(k, "input name %q must start with a letter or _ and contain only letters, digits, - and _", name)
				return
			}
			in := &Input{Type: "string"}
			what := "inputs." + name
			if !(v.Kind == yaml.ScalarNode && v.Tag == "!!null") {
				d.fields(v, what, []string{"description", "required", "default", "type", "options"}, func(key string, _, v *yaml.Node) {
					switch key {
					case "description":
						in.Description = d.str(v, what+".description")
					case "required":
						in.Required = d.boolean(v, what+".required")
					case "default":
						in.Default = d.str(v, what+".default")
					case "type":
						in.Type = d.str(v, what+".type")
					case "options":
						in.Options = d.strList(v, what+".options")
					}
				})
			}
			switch in.Type {
			case "string":
			case "boolean":
				if in.Default != "" && in.Default != "true" && in.Default != "false" {
					d.errorf(v, "%s.default must be true or false", what)
				}
			case "number":
				if in.Default != "" {
					if _, err := strconv.ParseFloat(in.Default, 64); err != nil {
						d.errorf(v, "%s.default must be a number", what)
					}
				}
			case "choice":
				if len(in.Options) == 0 {
					d.errorf(v, "%s: a choice input needs options", what)
				} else if in.Default != "" && !contains(in.Options, in.Default) {
					d.errorf(v, "%s.default %q isn't one of the options", what, in.Default)
				}
			default:
				d.errorf(v, "%s.type must be string, boolean, number or choice", what)
			}
			if in.Type != "choice" && in.Options != nil {
				d.errorf(v, "%s: options only apply to choice inputs", what)
			}
			t.Inputs[name] = in
			t.InputOrder = append(t.InputOrder, name)
		})
	})
	if len(t.Inputs) > MaxDispatchInputs {
		d.errorf(n, "on.workflow_dispatch: at most %d inputs", MaxDispatchInputs)
	}
	return t
}

func (d *decoder) concurrency(n *yaml.Node) *Concurrency {
	c := &Concurrency{}
	if n.Kind == yaml.ScalarNode {
		c.Group = n.Value
	} else {
		d.fields(n, "concurrency", []string{"group", "cancel-in-progress"}, func(key string, _, v *yaml.Node) {
			switch key {
			case "group":
				c.Group = d.str(v, "concurrency.group")
			case "cancel-in-progress":
				c.CancelInProgress = d.boolean(v, "concurrency.cancel-in-progress")
			}
		})
	}
	if strings.TrimSpace(c.Group) == "" {
		d.errorf(n, "concurrency: group is required")
	}
	d.checkExpr(n, "concurrency.group", c.Group, ctxConcurrency)
	return c
}

func (d *decoder) defaults(n *yaml.Node) Defaults {
	var out Defaults
	d.fields(n, "defaults", []string{"run"}, func(_ string, _, v *yaml.Node) {
		d.fields(v, "defaults.run", []string{"shell", "working-directory"}, func(key string, _, v *yaml.Node) {
			switch key {
			case "shell":
				out.Shell = d.shell(v, "defaults.run.shell")
			case "working-directory":
				out.WorkingDirectory = d.str(v, "defaults.run.working-directory")
			}
		})
	})
	return out
}

func (d *decoder) shell(n *yaml.Node, what string) string {
	s := d.str(n, what)
	if !shellRe.MatchString(s) && !strings.Contains(s, "{0}") {
		d.errorf(n, "%s: unknown shell %q (use bash, sh, python, pwsh, node, or a command containing {0})", what, s)
	}
	return s
}

func (d *decoder) container(n *yaml.Node, what string, allowed []string) Container {
	var c Container
	if n.Kind == yaml.ScalarNode {
		c.Image = n.Value
	} else {
		d.fields(n, what, []string{"image", "env"}, func(key string, _, v *yaml.Node) {
			switch key {
			case "image":
				c.Image = d.str(v, what+".image")
			case "env":
				c.Env = d.strMap(v, what+".env", envRe)
				for k, val := range c.Env {
					d.checkExpr(v, what+".env."+k, val, ctxStep)
				}
			}
		})
	}
	if strings.TrimSpace(c.Image) == "" {
		d.errorf(n, "%s: image is required", what)
	}
	d.checkExpr(n, what+".image", c.Image, allowed)
	return c
}

// ParseMemory parses a Docker-style memory size like 512m, 2g or 1.5GiB.
func ParseMemory(s string) (int64, error) {
	m := memRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (use a number with a unit, e.g. 512m or 4g)", s)
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	mult := float64(1)
	switch strings.ToLower(m[2][:1]) {
	case "k":
		mult = 1 << 10
	case "m":
		mult = 1 << 20
	case "g":
		mult = 1 << 30
	case "t":
		mult = 1 << 40
	}
	return int64(f * mult), nil
}

func (d *decoder) resources(n *yaml.Node, what string) Resources {
	var r Resources
	d.fields(n, what, []string{"cpus", "memory", "pids"}, func(key string, _, v *yaml.Node) {
		switch key {
		case "cpus":
			f, err := strconv.ParseFloat(v.Value, 64)
			if v.Kind != yaml.ScalarNode || err != nil || f <= 0 || f > 1024 {
				d.errorf(v, "%s.cpus must be a positive number like 2 or 0.5", what)
			}
			r.CPUs = f
		case "memory":
			b, err := ParseMemory(v.Value)
			if v.Kind != yaml.ScalarNode || err != nil {
				d.errorf(v, "%s.memory: %v", what, err)
			} else if b < 6<<20 {
				d.errorf(v, "%s.memory must be at least 6m", what)
			}
			r.MemoryBytes = b
		case "pids":
			r.Pids = d.integer(v, what+".pids", 16, 1<<22)
		}
	})
	return r
}

func (d *decoder) job(key string, n *yaml.Node) *Job {
	j := &Job{Key: key}
	what := "jobs." + key
	var stepsNode *yaml.Node
	d.fields(n, what, []string{"name", "runs-on", "container", "services", "needs", "if", "strategy", "env",
		"timeout-minutes", "continue-on-error", "resources", "outputs", "defaults", "steps", "permissions"}, func(field string, _, v *yaml.Node) {
		switch field {
		case "name":
			j.Name = d.str(v, what+".name")
			d.checkExpr(v, what+".name", j.Name, ctxJob)
		case "runs-on":
			j.RunsOn = d.strList(v, what+".runs-on")
			for _, l := range j.RunsOn {
				d.checkExpr(v, what+".runs-on", l, ctxJob)
			}
		case "container":
			c := d.container(v, what+".container", ctxJob)
			j.Container = &c
		case "services":
			j.Services = map[string]Container{}
			d.fields(v, what+".services", nil, func(name string, k, v *yaml.Node) {
				if !idRe.MatchString(name) {
					d.errorf(k, "service name %q must start with a letter or _ and contain only letters, digits, - and _", name)
					return
				}
				j.Services[name] = d.container(v, what+".services."+name, ctxJob)
			})
			if len(j.Services) > MaxServices {
				d.errorf(v, "%s.services: at most %d services", what, MaxServices)
			}
		case "needs":
			j.Needs = d.strList(v, what+".needs")
		case "if":
			j.If = d.str(v, what+".if")
			d.checkCond(v, what+".if", j.If, ctxJobIf)
		case "strategy":
			j.Strategy = d.strategy(v, what+".strategy")
		case "env":
			j.Env = d.strMap(v, what+".env", envRe)
			for k, val := range j.Env {
				d.checkExpr(v, what+".env."+k, val, ctxStep)
			}
		case "timeout-minutes":
			j.TimeoutMinutes = d.integer(v, what+".timeout-minutes", 1, 7*24*60)
		case "continue-on-error":
			j.ContinueOnError = d.boolean(v, what+".continue-on-error")
		case "resources":
			j.Resources = d.resources(v, what+".resources")
		case "outputs":
			j.Outputs = d.strMap(v, what+".outputs", idRe)
			for k, val := range j.Outputs {
				d.checkExpr(v, what+".outputs."+k, val, ctxJobOutputs)
			}
		case "defaults":
			j.Defaults = d.defaults(v)
		case "steps":
			stepsNode = v
		}
	})
	if stepsNode == nil {
		d.errorf(n, "%s: steps are required", what)
		return j
	}
	if stepsNode.Kind != yaml.SequenceNode {
		d.errorf(stepsNode, "%s.steps must be a list", what)
		return j
	}
	if len(stepsNode.Content) == 0 {
		d.errorf(stepsNode, "%s: the job has no steps", what)
	}
	if len(stepsNode.Content) > MaxStepsPerJob {
		d.errorf(stepsNode, "%s: at most %d steps per job", what, MaxStepsPerJob)
	}
	ids := map[string]bool{}
	for i, sn := range stepsNode.Content {
		s := d.step(sn, fmt.Sprintf("%s.steps[%d]", what, i))
		if s.ID != "" {
			if ids[s.ID] {
				d.errorf(sn, "%s.steps[%d]: duplicate step id %q", what, i, s.ID)
			}
			ids[s.ID] = true
		}
		j.Steps = append(j.Steps, s)
	}
	return j
}

func (d *decoder) step(n *yaml.Node, what string) Step {
	var s Step
	var usesNode *yaml.Node
	hasRun := false
	d.fields(n, what, []string{"id", "name", "if", "run", "shell", "working-directory", "uses", "with", "env", "continue-on-error", "timeout-minutes"}, func(key string, _, v *yaml.Node) {
		switch key {
		case "id":
			s.ID = d.str(v, what+".id")
			if !idRe.MatchString(s.ID) {
				d.errorf(v, "%s.id %q must start with a letter or _ and contain only letters, digits, - and _", what, s.ID)
			}
		case "name":
			s.Name = d.str(v, what+".name")
			d.checkExpr(v, what+".name", s.Name, ctxStep)
		case "if":
			s.If = d.str(v, what+".if")
			d.checkCond(v, what+".if", s.If, ctxStep)
		case "run":
			hasRun = true
			s.Run = d.str(v, what+".run")
			d.checkExpr(v, what+".run", s.Run, ctxStep)
		case "shell":
			s.Shell = d.shell(v, what+".shell")
		case "working-directory":
			s.WorkingDirectory = d.str(v, what+".working-directory")
			d.checkExpr(v, what+".working-directory", s.WorkingDirectory, ctxStep)
		case "uses":
			usesNode = v
			raw := d.str(v, what+".uses")
			if s.Uses = builtinName(raw); s.Uses == "" {
				names := make([]string, 0, len(Builtins))
				for k := range Builtins {
					names = append(names, k)
				}
				sort.Strings(names)
				d.errorf(v, "%s.uses: %q isn't a built-in step (available: %s); third-party actions aren't supported, use run: instead", what, raw, strings.Join(names, ", "))
			}
		case "with":
			s.With = d.strMap(v, what+".with", nil)
			for k, val := range s.With {
				d.checkExpr(v, what+".with."+k, val, ctxStep)
			}
		case "env":
			s.Env = d.strMap(v, what+".env", envRe)
			for k, val := range s.Env {
				d.checkExpr(v, what+".env."+k, val, ctxStep)
			}
		case "continue-on-error":
			s.ContinueOnError = d.boolean(v, what+".continue-on-error")
		case "timeout-minutes":
			s.TimeoutMinutes = d.integer(v, what+".timeout-minutes", 1, 7*24*60)
		}
	})
	switch {
	case hasRun && usesNode != nil:
		d.errorf(n, "%s: a step has either run or uses, not both", what)
	case !hasRun && usesNode == nil:
		d.errorf(n, "%s: a step needs run or uses", what)
	case hasRun && strings.TrimSpace(s.Run) == "":
		d.errorf(n, "%s.run is empty", what)
	}
	if s.Uses != "" {
		b := Builtins[s.Uses]
		for k := range s.With {
			if _, ok := b.With[k]; !ok {
				d.errorf(n, "%s.with: %s doesn't take %q", what, s.Uses, k)
			}
		}
		for k, req := range b.With {
			if _, ok := s.With[k]; req && !ok {
				d.errorf(n, "%s.with: %s needs %q", what, s.Uses, k)
			}
		}
		if s.Shell != "" {
			d.errorf(n, "%s: shell only applies to run steps", what)
		}
	} else if s.With != nil {
		d.errorf(n, "%s: with only applies to uses steps", what)
	}
	return s
}

func (d *decoder) strategy(n *yaml.Node, what string) *Strategy {
	st := &Strategy{FailFast: true}
	d.fields(n, what, []string{"matrix", "fail-fast", "max-parallel"}, func(key string, _, v *yaml.Node) {
		switch key {
		case "fail-fast":
			st.FailFast = d.boolean(v, what+".fail-fast")
		case "max-parallel":
			st.MaxParallel = d.integer(v, what+".max-parallel", 1, MaxMatrixJobs)
		case "matrix":
			st.Matrix = map[string][]any{}
			d.fields(v, what+".matrix", nil, func(dim string, k, v *yaml.Node) {
				switch dim {
				case "include", "exclude":
					if v.Kind != yaml.SequenceNode {
						d.errorf(v, "%s.matrix.%s must be a list of mappings", what, dim)
						return
					}
					for _, e := range v.Content {
						var m map[string]any
						if e.Kind != yaml.MappingNode || e.Decode(&m) != nil {
							d.errorf(e, "%s.matrix.%s entries must be mappings", what, dim)
							continue
						}
						nm, _ := expr.Normalize(m).(map[string]any)
						if dim == "include" {
							st.Include = append(st.Include, nm)
						} else {
							st.Exclude = append(st.Exclude, nm)
						}
					}
					return
				}
				if !idRe.MatchString(dim) {
					d.errorf(k, "matrix dimension %q must start with a letter or _ and contain only letters, digits, - and _", dim)
					return
				}
				var vals []any
				if v.Kind != yaml.SequenceNode || v.Decode(&vals) != nil {
					d.errorf(v, "%s.matrix.%s must be a list of values", what, dim)
					return
				}
				if len(vals) == 0 {
					d.errorf(v, "%s.matrix.%s is empty", what, dim)
					return
				}
				st.Matrix[dim] = expr.Normalize(vals).([]any)
				st.MatrixKeys = append(st.MatrixKeys, dim)
			})
			if len(st.Matrix) == 0 && len(st.Include) == 0 {
				d.errorf(v, "%s.matrix has no values", what)
			}
			for _, ex := range st.Exclude {
				for k := range ex {
					if _, ok := st.Matrix[k]; !ok {
						d.errorf(v, "%s.matrix.exclude: %q isn't a matrix dimension", what, k)
					}
				}
			}
			if n := len(ExpandMatrix(st)); n > MaxMatrixJobs {
				d.errorf(v, "%s.matrix produces %d jobs; the limit is %d", what, n, MaxMatrixJobs)
			}
		}
	})
	return st
}

// checkNeeds validates needs references and rejects cycles.
func (d *decoder) checkNeeds(w *Workflow, jobsNode *yaml.Node) {
	for _, key := range w.JobOrder {
		j := w.Jobs[key]
		for _, need := range j.Needs {
			if need == key {
				d.errorf(jobsNode, "jobs.%s needs itself", key)
			} else if _, ok := w.Jobs[need]; !ok {
				d.errorf(jobsNode, "jobs.%s needs %q, which isn't a job in this workflow", key, need)
			}
		}
	}
	if len(d.errs) > 0 {
		return
	}
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(k string, path []string) bool
	visit = func(k string, path []string) bool {
		switch state[k] {
		case 1:
			d.errorf(jobsNode, "jobs: dependency cycle: %s", strings.Join(append(path, k), " → "))
			return false
		case 2:
			return true
		}
		state[k] = 1
		for _, n := range w.Jobs[k].Needs {
			if !visit(n, append(path, k)) {
				return false
			}
		}
		state[k] = 2
		return true
	}
	for _, k := range w.JobOrder {
		if !visit(k, nil) {
			return
		}
	}
}
