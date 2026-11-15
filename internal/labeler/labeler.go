// Package labeler implements tgp's built-in pull request labeler: labels
// chosen from the files a pull request changes and its branch names,
// configured like actions/labeler v5 (and its v4 shorthand).
//
//	# .tgp/labeler.yml (or .github/labeler.yml)
//	issues:
//	  - changed-files:
//	      - any-glob-to-any-file: ['services/issues/**']
//	docs:
//	  - all:
//	      - changed-files:
//	          - any-glob-to-any-file: '**/*.md'
//	          - all-globs-to-all-files: ['!src/**']
//	feature:
//	  - head-branch: ['^feature/', 'feature']
//	ci: '.tgp/**'          # v4 shorthand: any glob matches any file
//
// As in actions/labeler v5: changed-files, head-branch and base-branch
// rules written directly under a label form one any: group (one of them,
// and one option or pattern within each, is enough); each any: and all:
// entry must match too; inside all: every rule and every option or
// pattern must match.
package labeler

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Paths are where the configuration is read from, first match wins.
var Paths = []string{".tgp/labeler.yml", ".tgp/labeler.yaml", ".github/labeler.yml", ".github/labeler.yaml"}

// MaxFileBytes caps the configuration file.
const MaxFileBytes = 256 << 10

// Config is a parsed labeler configuration.
type Config struct {
	Labels []Label
	// SyncLabels removes configured labels that no longer match.
	SyncLabels bool
}

// Label is one label and the rules that apply it.
type Label struct {
	Name string
	// Entries must all match.
	Entries []Rule
	Line    int
}

// Rule is a match rule: a group (any/all of sub-rules) or a leaf.
type Rule struct {
	Any, All []Rule
	// Leaves (one of them set).
	ChangedFiles []FileRule
	HeadBranch   []*regexp.Regexp
	BaseBranch   []*regexp.Regexp
}

// FileRule is one changed-files option.
type FileRule struct {
	Kind  string // any-glob-to-any-file, any-glob-to-all-files, all-globs-to-any-file, all-globs-to-all-files
	Globs []*Glob
}

var fileKinds = []string{"any-glob-to-any-file", "any-glob-to-all-files", "all-globs-to-any-file", "all-globs-to-all-files"}

// Error is a configuration problem with its position.
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

type parser struct {
	file string
	errs Errors
}

func (p *parser) errorf(n *yaml.Node, format string, args ...any) {
	e := &Error{File: p.file, Msg: fmt.Sprintf(format, args...)}
	if n != nil {
		e.Line, e.Col = n.Line, n.Column
	}
	p.errs = append(p.errs, e)
}

// Parse parses and validates a labeler configuration. file names it in
// errors.
func Parse(file string, data []byte) (*Config, error) {
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
	p := &parser{file: file}
	c := &Config{}
	if len(doc.Content) == 0 {
		return c, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		p.errorf(root, "the labeler configuration must map label names to rules")
		return nil, p.errs
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Value == "sync-labels" && v.Kind == yaml.ScalarNode && (v.Value == "true" || v.Value == "false") {
			c.SyncLabels = v.Value == "true"
			continue
		}
		name := strings.TrimSpace(k.Value)
		if name == "" {
			p.errorf(k, "empty label name")
			continue
		}
		if seen[strings.ToLower(name)] {
			p.errorf(k, "label %q is configured twice", name)
			continue
		}
		seen[strings.ToLower(name)] = true
		l := Label{Name: name, Line: k.Line}
		switch v.Kind {
		case yaml.ScalarNode, yaml.SequenceNode:
			if isShorthand(v) {
				// v4: a glob or a list of globs (any glob, any file).
				globs := p.globs(v, name)
				l.Entries = []Rule{{ChangedFiles: []FileRule{{Kind: "any-glob-to-any-file", Globs: globs}}}}
				break
			}
			var loose []Rule // top-level leaves: one implicit any: group
			for _, e := range v.Content {
				if e.Kind != yaml.MappingNode {
					p.errorf(e, "%s: each rule must be a mapping (changed-files, head-branch, base-branch, any or all)", name)
					continue
				}
				for j := 0; j+1 < len(e.Content); j += 2 {
					one := &yaml.Node{Kind: yaml.MappingNode, Line: e.Line, Column: e.Column, Content: e.Content[j : j+2]}
					if k := e.Content[j].Value; k == "any" || k == "all" {
						l.Entries = append(l.Entries, p.rule(one, name, "all"))
					} else {
						loose = append(loose, p.rule(one, name, "any"))
					}
				}
			}
			if len(loose) > 0 {
				l.Entries = append(l.Entries, Rule{Any: loose})
			}
		default:
			p.errorf(v, "%s: rules must be a list", name)
		}
		if len(l.Entries) == 0 {
			p.errorf(v, "%s: no rules", name)
		}
		c.Labels = append(c.Labels, l)
	}
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	return c, nil
}

// isShorthand: a scalar, or a list of scalars, is the v4 glob list.
func isShorthand(v *yaml.Node) bool {
	if v.Kind == yaml.ScalarNode {
		return true
	}
	for _, c := range v.Content {
		if c.Kind != yaml.ScalarNode {
			return false
		}
	}
	return len(v.Content) > 0
}

// rule parses a match object. mode is how leaves inside combine (all for
// top level and all:, any for any:).
func (p *parser) rule(n *yaml.Node, label, mode string) Rule {
	var r Rule
	if n.Kind != yaml.MappingNode {
		p.errorf(n, "%s: each rule must be a mapping (changed-files, head-branch, base-branch, any or all)", label)
		return r
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		switch k.Value {
		case "any", "all":
			if v.Kind != yaml.SequenceNode {
				p.errorf(v, "%s.%s must be a list", label, k.Value)
				continue
			}
			var subs []Rule
			for _, c := range v.Content {
				subs = append(subs, p.rule(c, label, k.Value))
			}
			if k.Value == "any" {
				r.All = append(r.All, Rule{Any: subs})
			} else {
				r.All = append(r.All, Rule{All: subs})
			}
		case "changed-files":
			var opts []FileRule
			items := v.Content
			if v.Kind != yaml.SequenceNode {
				items = []*yaml.Node{v}
			}
			for _, it := range items {
				if it.Kind != yaml.MappingNode {
					p.errorf(it, "%s.changed-files entries must be mappings like any-glob-to-any-file: [globs]", label)
					continue
				}
				for j := 0; j+1 < len(it.Content); j += 2 {
					kk, vv := it.Content[j], it.Content[j+1]
					if !contains(fileKinds, kk.Value) {
						p.errorf(kk, "%s.changed-files: unknown option %q (use %s)", label, kk.Value, strings.Join(fileKinds, ", "))
						continue
					}
					opts = append(opts, FileRule{Kind: kk.Value, Globs: p.globs(vv, label)})
				}
			}
			r.All = append(r.All, leaf(mode, Rule{ChangedFiles: opts}))
		case "head-branch", "base-branch":
			res := p.regexps(v, label+"."+k.Value)
			lr := Rule{HeadBranch: res}
			if k.Value == "base-branch" {
				lr = Rule{BaseBranch: res}
			}
			r.All = append(r.All, leaf(mode, lr))
		default:
			p.errorf(k, "%s: unknown key %q (use changed-files, head-branch, base-branch, any or all)", label, k.Value)
		}
	}
	return r
}

// leaf wraps a leaf so its options combine per mode: under any: one
// option is enough, elsewhere every option must match.
func leaf(mode string, r Rule) Rule {
	if mode == "any" {
		return Rule{Any: []Rule{r}, All: nil}
	}
	return r
}

func (p *parser) strs(n *yaml.Node, what string) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				p.errorf(c, "%s must be a string or a list of strings", what)
				continue
			}
			out = append(out, c.Value)
		}
		return out
	}
	p.errorf(n, "%s must be a string or a list of strings", what)
	return nil
}

func (p *parser) globs(n *yaml.Node, label string) []*Glob {
	var out []*Glob
	for _, s := range p.strs(n, label+" globs") {
		g, err := CompileGlob(s)
		if err != nil {
			p.errorf(n, "%s: invalid glob %q: %v", label, s, err)
			continue
		}
		out = append(out, g)
	}
	return out
}

func (p *parser) regexps(n *yaml.Node, what string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, s := range p.strs(n, what) {
		re, err := regexp.Compile(s)
		if err != nil {
			p.errorf(n, "%s: invalid regular expression %q: %v", what, s, err)
			continue
		}
		out = append(out, re)
	}
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// PR is what rules look at.
type PR struct {
	Files      []string
	HeadBranch string
	BaseBranch string
}

// Match returns the names of the labels whose rules match pr, sorted.
func (c *Config) Match(pr PR) []string {
	var out []string
	for _, l := range c.Labels {
		ok := len(l.Entries) > 0
		for _, e := range l.Entries {
			if !e.match(pr, false) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, l.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Names lists the configured label names.
func (c *Config) Names() []string {
	out := make([]string, len(c.Labels))
	for i, l := range c.Labels {
		out[i] = l.Name
	}
	return out
}

// match evaluates a rule. Leaves match when every option matches, or, with
// anyMode (inside any:), when one does.
func (r Rule) match(pr PR, anyMode bool) bool {
	switch {
	case r.Any != nil:
		for _, s := range r.Any {
			if s.match(pr, true) {
				return true
			}
		}
		return false
	case r.All != nil:
		for _, s := range r.All {
			if !s.match(pr, false) {
				return false
			}
		}
		return true
	case r.ChangedFiles != nil:
		return combine(len(r.ChangedFiles), anyMode, func(i int) bool { return r.ChangedFiles[i].match(pr.Files) })
	case r.HeadBranch != nil:
		return combine(len(r.HeadBranch), anyMode, func(i int) bool { return r.HeadBranch[i].MatchString(pr.HeadBranch) })
	case r.BaseBranch != nil:
		return combine(len(r.BaseBranch), anyMode, func(i int) bool { return r.BaseBranch[i].MatchString(pr.BaseBranch) })
	}
	return false
}

func combine(n int, anyMode bool, f func(int) bool) bool {
	if n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if f(i) == anyMode {
			return anyMode
		}
	}
	return !anyMode
}

func (f FileRule) match(files []string) bool {
	if len(f.Globs) == 0 || len(files) == 0 {
		return false
	}
	switch f.Kind {
	case "any-glob-to-any-file":
		for _, g := range f.Globs {
			for _, file := range files {
				if g.Match(file) {
					return true
				}
			}
		}
		return false
	case "any-glob-to-all-files":
		for _, g := range f.Globs {
			all := true
			for _, file := range files {
				if !g.Match(file) {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
		return false
	case "all-globs-to-any-file":
		for _, file := range files {
			all := true
			for _, g := range f.Globs {
				if !g.Match(file) {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
		return false
	case "all-globs-to-all-files":
		for _, g := range f.Globs {
			for _, file := range files {
				if !g.Match(file) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// Glob is a minimatch-style pattern (as actions/labeler uses, with dot
// files matched): * and ? stay within a path segment, ** spans segments,
// [abc] classes, {a,b} alternatives, and a leading ! negates.
type Glob struct {
	src    string
	negate bool
	re     *regexp.Regexp
}

// String returns the pattern.
func (g *Glob) String() string { return g.src }

// Match reports whether path matches the glob.
func (g *Glob) Match(path string) bool { return g.re.MatchString(path) != g.negate }

// CompileGlob compiles a pattern.
func CompileGlob(pattern string) (*Glob, error) {
	g := &Glob{src: pattern}
	p := pattern
	for strings.HasPrefix(p, "!") {
		g.negate = !g.negate
		p = p[1:]
	}
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	alts, err := expandBraces(p, 0)
	if err != nil {
		return nil, err
	}
	parts := make([]string, len(alts))
	for i, a := range alts {
		re, err := globToRegexp(a)
		if err != nil {
			return nil, err
		}
		parts[i] = re
	}
	re, err := regexp.Compile("^(?:" + strings.Join(parts, "|") + ")$")
	if err != nil {
		return nil, err
	}
	g.re = re
	return g, nil
}

// expandBraces turns a{b,c}d into abd, acd (nested braces work).
func expandBraces(p string, depth int) ([]string, error) {
	if depth > 10 {
		return nil, fmt.Errorf("braces nested too deeply")
	}
	start := -1
	level := 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '{':
			if level == 0 {
				start = i
			}
			level++
		case '}':
			if level == 0 {
				continue
			}
			level--
			if level == 0 {
				var opts []string
				lvl, last := 0, start+1
				for j := start + 1; j < i; j++ {
					switch p[j] {
					case '\\':
						j++
					case '{':
						lvl++
					case '}':
						lvl--
					case ',':
						if lvl == 0 {
							opts = append(opts, p[last:j])
							last = j + 1
						}
					}
				}
				opts = append(opts, p[last:i])
				if len(opts) == 1 {
					// {a} is literal in minimatch.
					rest, err := expandBraces(p[i+1:], depth)
					if err != nil {
						return nil, err
					}
					var out []string
					for _, r := range rest {
						out = append(out, p[:i+1]+r)
					}
					return out, nil
				}
				var out []string
				for _, o := range opts {
					sub, err := expandBraces(p[:start]+o+p[i+1:], depth+1)
					if err != nil {
						return nil, err
					}
					out = append(out, sub...)
					if len(out) > 256 {
						return nil, fmt.Errorf("too many brace alternatives")
					}
				}
				return out, nil
			}
		}
	}
	if level != 0 {
		return []string{p}, nil // an unclosed brace is literal
	}
	return []string{p}, nil
}

func globToRegexp(p string) (string, error) {
	var b strings.Builder
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		last := i == len(segs)-1
		if seg == "**" {
			switch {
			case last && i == 0:
				b.WriteString(".*")
			case last:
				// a/** matches everything below a (and a itself, as in minimatch).
				s := b.String()
				b.Reset()
				b.WriteString(strings.TrimSuffix(s, "/"))
				b.WriteString("(?:/.*)?")
			default:
				b.WriteString("(?:[^/]*/)*")
			}
			continue
		}
		for j := 0; j < len(seg); j++ {
			c := seg[j]
			switch c {
			case '*':
				b.WriteString("[^/]*")
			case '?':
				b.WriteString("[^/]")
			case '\\':
				if j+1 < len(seg) {
					j++
					b.WriteString(regexp.QuoteMeta(string(seg[j])))
				}
			case '[':
				end := strings.IndexByte(seg[j+1:], ']')
				if end < 0 {
					b.WriteString(`\[`)
					continue
				}
				class := seg[j+1 : j+1+end]
				if strings.HasPrefix(class, "!") {
					class = "^" + class[1:]
				}
				class = strings.ReplaceAll(class, `\`, `\\`)
				if _, err := regexp.Compile("[" + class + "]"); err != nil {
					return "", fmt.Errorf("bad character class [%s]", seg[j+1:j+1+end])
				}
				b.WriteString("[" + class + "]")
				j += end + 1
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		if !last {
			b.WriteString("/")
		}
	}
	return b.String(), nil
}
