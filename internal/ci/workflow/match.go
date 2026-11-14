package workflow

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/expr"
)

// Event is something that may start workflows.
type Event struct {
	// Name is push, pull_request or workflow_dispatch.
	Name string
	// Ref is the full ref pushed (refs/heads/x, refs/tags/v1) for push.
	Ref string
	// BaseBranch is the target branch of a pull request.
	BaseBranch string
	// Action is the pull request activity (opened, synchronize, ...).
	Action string
	// Actions are the release activities (published, released or
	// prereleased); a workflow matches if it listens for any of them.
	Actions []string
	// Changed lists changed paths. It's called at most once, only when a
	// workflow filters on paths. Nil means unknown: path filters pass.
	Changed func() ([]string, error)
}

// Matches reports whether the event triggers the workflow.
func (w *Workflow) Matches(ev *Event) (bool, error) {
	switch ev.Name {
	case "push":
		t := w.On.Push
		if t == nil {
			return false, nil
		}
		switch {
		case strings.HasPrefix(ev.Ref, "refs/heads/"):
			if t.Tags != nil || t.TagsIgnore != nil {
				if t.Branches == nil && t.BranchesIgnore == nil {
					return false, nil // only tag filters: branch pushes don't run
				}
			}
			if !refFilter(strings.TrimPrefix(ev.Ref, "refs/heads/"), t.Branches, t.BranchesIgnore) {
				return false, nil
			}
		case strings.HasPrefix(ev.Ref, "refs/tags/"):
			if t.Branches != nil || t.BranchesIgnore != nil {
				if t.Tags == nil && t.TagsIgnore == nil {
					return false, nil
				}
			}
			// Path filters don't apply to tag pushes.
			return refFilter(strings.TrimPrefix(ev.Ref, "refs/tags/"), t.Tags, t.TagsIgnore), nil
		default:
			return false, nil
		}
		return pathFilter(ev, t.Paths, t.PathsIgnore)
	case "pull_request":
		t := w.On.PullRequest
		if t == nil {
			return false, nil
		}
		types := t.Types
		if types == nil {
			types = DefaultPullTypes
		}
		if !contains(types, ev.Action) {
			return false, nil
		}
		if !refFilter(ev.BaseBranch, t.Branches, t.BranchesIgnore) {
			return false, nil
		}
		return pathFilter(ev, t.Paths, t.PathsIgnore)
	case "workflow_dispatch":
		return w.On.Dispatch != nil, nil
	case "release":
		t := w.On.Release
		if t == nil {
			return false, nil
		}
		types := t.Types
		if types == nil {
			types = DefaultReleaseTypes
		}
		for _, a := range ev.Actions {
			if contains(types, a) {
				return true, nil
			}
		}
	}
	return false, nil
}

func refFilter(name string, include, ignore []string) bool {
	if include != nil {
		return matchList(include, name)
	}
	if ignore != nil {
		return !matchList(ignore, name)
	}
	return true
}

func pathFilter(ev *Event, include, ignore []string) (bool, error) {
	if include == nil && ignore == nil {
		return true, nil
	}
	if ev.Changed == nil {
		return true, nil
	}
	files, err := ev.Changed()
	if err != nil {
		return false, err
	}
	if include != nil {
		for _, f := range files {
			if matchList(include, f) {
				return true, nil
			}
		}
		return false, nil
	}
	for _, f := range files {
		if !matchList(ignore, f) {
			return true, nil
		}
	}
	return false, nil
}

// matchList applies a pattern list in order: a later "!pattern" can exclude
// what an earlier pattern matched, and vice versa.
func matchList(patterns []string, s string) bool {
	matched := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		re, err := globRegexp(strings.TrimPrefix(p, "!"))
		if err != nil {
			continue
		}
		if re.MatchString(s) {
			matched = !neg
		}
	}
	return matched
}

// MatchGlob reports whether s matches a filter pattern.
func MatchGlob(pattern, s string) bool {
	re, err := globRegexp(pattern)
	return err == nil && re.MatchString(s)
}

// globRegexp compiles a filter pattern with GitHub's syntax: * matches
// anything but /, ** matches anything, ? and + repeat the preceding
// character (zero-or-one, one-or-more), [..] is a character class and \
// escapes.
func globRegexp(p string) (*regexp.Regexp, error) {
	if p == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?', '+':
			if i == 0 {
				return nil, fmt.Errorf("%q can't start a pattern", string(c))
			}
			b.WriteByte(c)
		case '[':
			j := strings.IndexByte(p[i:], ']')
			if j < 0 {
				return nil, fmt.Errorf("unclosed [")
			}
			b.WriteString(p[i : i+j+1])
			i += j
		case '\\':
			if i+1 >= len(p) {
				return nil, fmt.Errorf("trailing \\")
			}
			b.WriteString(regexp.QuoteMeta(string(p[i+1])))
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// ---- matrix ----

// ExpandMatrix returns the strategy's job combinations, following GitHub's
// rules: the cartesian product of the dimensions, minus exclude entries,
// plus include entries (merged into matching combinations without
// overwriting original values, or added as new combinations).
func ExpandMatrix(st *Strategy) []map[string]any {
	if st == nil {
		return nil
	}
	var combos []map[string]any
	if len(st.MatrixKeys) > 0 {
		combos = []map[string]any{{}}
		for _, k := range st.MatrixKeys {
			var next []map[string]any
			for _, c := range combos {
				for _, v := range st.Matrix[k] {
					n := make(map[string]any, len(c)+1)
					for ck, cv := range c {
						n[ck] = cv
					}
					n[k] = v
					next = append(next, n)
					if len(next) > MaxMatrixJobs*4 {
						break
					}
				}
			}
			combos = next
		}
	}
	kept := combos[:0]
	for _, c := range combos {
		excluded := false
		for _, ex := range st.Exclude {
			if subsetOf(ex, c) {
				excluded = true
				break
			}
		}
		if !excluded {
			kept = append(kept, c)
		}
	}
	combos = kept
	originals := len(combos)
	for _, inc := range st.Include {
		matched := false
		for i := 0; i < originals; i++ {
			c := combos[i]
			ok := true
			for k, v := range inc {
				if _, orig := st.Matrix[k]; orig && !reflect.DeepEqual(c[k], v) {
					ok = false
					break
				}
			}
			if ok {
				for k, v := range inc {
					c[k] = v
				}
				matched = true
			}
		}
		if !matched {
			n := make(map[string]any, len(inc))
			for k, v := range inc {
				n[k] = v
			}
			combos = append(combos, n)
		}
	}
	return combos
}

func subsetOf(sub, m map[string]any) bool {
	for k, v := range sub {
		if !reflect.DeepEqual(m[k], v) {
			return false
		}
	}
	return true
}

// Instance is one concrete job from a job definition (one per matrix
// combination, or exactly one without a matrix).
type Instance struct {
	Key    string         `json:"key"`
	Name   string         `json:"name"`
	Matrix map[string]any `json:"matrix,omitempty"`
}

// Instances expands a job into its concrete instances. c supplies the
// github and inputs contexts used to evaluate names.
func (j *Job) Instances(c *expr.Context) ([]Instance, error) {
	if j.Strategy == nil {
		name := j.Key
		if j.Name != "" {
			n, err := expr.Interpolate(j.Name, c)
			if err != nil {
				return nil, err
			}
			name = n
		}
		return []Instance{{Key: j.Key, Name: name}}, nil
	}
	combos := ExpandMatrix(j.Strategy)
	if len(combos) == 0 {
		return nil, fmt.Errorf("jobs.%s: the matrix produces no jobs", j.Key)
	}
	out := make([]Instance, 0, len(combos))
	for _, m := range combos {
		base := j.Key
		if j.Name != "" {
			mc := withContext(c, "matrix", m)
			n, err := expr.Interpolate(j.Name, mc)
			if err != nil {
				return nil, err
			}
			base = n
		}
		name := base
		// Like GitHub: append the matrix values unless the name already
		// uses the matrix.
		if !strings.Contains(j.Name, "matrix.") {
			name = base + " (" + matrixLabel(j.Strategy, m) + ")"
		}
		out = append(out, Instance{Key: j.Key, Name: name, Matrix: m})
	}
	return out, nil
}

func matrixLabel(st *Strategy, m map[string]any) string {
	var keys []string
	keys = append(keys, st.MatrixKeys...)
	var extra []string
	for k := range m {
		if _, ok := st.Matrix[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := expr.String(v)
			s = strings.Join(strings.Fields(s), " ")
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// withContext returns a copy of c with one context replaced.
func withContext(c *expr.Context, name string, v any) *expr.Context {
	n := &expr.Context{Contexts: map[string]any{}}
	if c != nil {
		for k, x := range c.Contexts {
			n.Contexts[k] = x
		}
		n.Status = c.Status
	}
	n.Contexts[name] = v
	return n
}

// WithContext is withContext for other packages.
func WithContext(c *expr.Context, name string, v any) *expr.Context { return withContext(c, name, v) }
