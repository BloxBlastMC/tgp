// Package expr evaluates the ${{ … }} expression language used in workflow
// files. It follows GitHub Actions semantics: loose equality with numeric
// coercion, case-insensitive string comparison, && and || returning operand
// values, property dereference with a.b, a['b'] and the a.*.b filter, and the
// built-in functions contains, startsWith, endsWith, format, join, toJSON,
// fromJSON and the status functions success, failure, cancelled and always.
package expr

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Context holds the values available to an expression. Contexts maps names
// like "github", "env", "matrix" to values (map[string]any, []any, string,
// float64, bool or nil). Status answers the status functions; when nil they
// behave as if every previous step or job succeeded.
type Context struct {
	Contexts map[string]any
	Status   func(fn string) bool
	// HashFiles implements hashFiles(patterns...): a hash of the matching
	// files, or "" when none match. Nil (outside a job) means "".
	HashFiles func(patterns []string) string
}

// Error is a parse or evaluation error.
type Error struct {
	Expr string
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("expression %q: %s", e.Expr, e.Msg) }

// Expr is a parsed expression.
type Expr struct {
	src  string
	root node
}

// Parse parses an expression (without the ${{ }} wrapper).
func Parse(src string) (*Expr, error) {
	p := &parser{src: src}
	if err := p.lex(); err != nil {
		return nil, &Error{Expr: src, Msg: err.Error()}
	}
	n, err := p.expr(0)
	if err != nil {
		return nil, &Error{Expr: src, Msg: err.Error()}
	}
	if p.peek().kind != tEOF {
		return nil, &Error{Expr: src, Msg: fmt.Sprintf("unexpected %q", p.peek().text)}
	}
	return &Expr{src: src, root: n}, nil
}

// Eval evaluates the expression.
func (e *Expr) Eval(c *Context) (v any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if ee, ok := r.(evalErr); ok {
				err = &Error{Expr: e.src, Msg: string(ee)}
				return
			}
			panic(r)
		}
	}()
	return e.root.eval(c), nil
}

// UsesStatus reports whether the expression calls a status function.
func (e *Expr) UsesStatus() bool { return usesStatus(e.root) }

// Contexts lists the top-level context names the expression reads.
func (e *Expr) Contexts() []string {
	seen := map[string]bool{}
	walk(e.root, func(n node) {
		if id, ok := n.(*ident); ok {
			seen[strings.ToLower(id.name)] = true
		}
	})
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Eval parses and evaluates src.
func Eval(src string, c *Context) (any, error) {
	e, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return e.Eval(c)
}

// Condition evaluates an if: condition. The ${{ }} wrapper is optional, an
// empty condition means success(), and a condition without a status
// function is implicitly "success() && (cond)".
func Condition(cond string, c *Context) (bool, error) {
	cond = strings.TrimSpace(cond)
	if strings.HasPrefix(cond, "${{") && strings.HasSuffix(cond, "}}") && strings.Count(cond, "${{") == 1 {
		cond = strings.TrimSpace(cond[3 : len(cond)-2])
	}
	if cond == "" {
		cond = "success()"
	}
	e, err := Parse(cond)
	if err != nil {
		return false, err
	}
	if !e.UsesStatus() {
		if !status(c, "success") {
			return false, nil
		}
	}
	v, err := e.Eval(c)
	if err != nil {
		return false, err
	}
	return Truthy(v), nil
}

// CheckCondition parses an if: condition without evaluating it.
func CheckCondition(cond string) error {
	cond = strings.TrimSpace(cond)
	if strings.HasPrefix(cond, "${{") && strings.HasSuffix(cond, "}}") && strings.Count(cond, "${{") == 1 {
		cond = strings.TrimSpace(cond[3 : len(cond)-2])
	}
	if cond == "" {
		return nil
	}
	_, err := Parse(cond)
	return err
}

// Interpolate replaces every ${{ expr }} in s with the string form of its
// value.
func Interpolate(s string, c *Context) (string, error) {
	if !strings.Contains(s, "${{") {
		return s, nil
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		b.WriteString(s[:i])
		end := findClose(s[i+3:])
		if end < 0 {
			return "", &Error{Expr: s[i:], Msg: "missing closing }}"}
		}
		v, err := Eval(s[i+3:i+3+end], c)
		if err != nil {
			return "", err
		}
		b.WriteString(String(v))
		s = s[i+3+end+2:]
	}
}

// Check parses every ${{ }} in s without evaluating, returning the first
// error and the context names used.
func Check(s string) ([]string, error) {
	var names []string
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			return names, nil
		}
		end := findClose(s[i+3:])
		if end < 0 {
			return names, &Error{Expr: s[i:], Msg: "missing closing }}"}
		}
		e, err := Parse(s[i+3 : i+3+end])
		if err != nil {
			return names, err
		}
		names = append(names, e.Contexts()...)
		s = s[i+3+end+2:]
	}
}

// findClose finds the }} ending an expression, skipping string literals.
func findClose(s string) int {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'':
			if inStr && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			inStr = !inStr
		case !inStr && s[i] == '}' && i+1 < len(s) && s[i+1] == '}':
			return i
		}
	}
	return -1
}

// ---- values ----

// Truthy implements expression truthiness: false, 0, -0, NaN, "" and null
// are falsy; everything else (including empty objects and arrays) is truthy.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// String converts a value to its string form for interpolation.
func String(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(x)
	case string:
		return x
	}
	b, _ := json.MarshalIndent(normalize(v), "", "  ")
	return string(b)
}

func formatNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == math.Trunc(f) && math.Abs(f) < 1e15:
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func toNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0
		}
		if strings.HasPrefix(s, "0x") {
			if n, err := strconv.ParseInt(s[2:], 16, 64); err == nil {
				return float64(n)
			}
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return math.NaN()
	}
	return math.NaN()
}

// Normalize converts Go values (ints, typed maps and slices) to the
// expression value types.
func Normalize(v any) any { return normalize(v) }

func normalize(v any) any {
	switch x := v.(type) {
	case nil, bool, float64, string:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = normalize(e)
		}
		return m
	case map[string]string:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = e
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, e := range x {
			a[i] = normalize(e)
		}
		return a
	case []string:
		a := make([]any, len(x))
		for i, e := range x {
			a[i] = e
		}
		return a
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func kind(v any) int {
	switch v.(type) {
	case nil:
		return 0
	case bool:
		return 1
	case float64:
		return 2
	case string:
		return 3
	}
	return 4
}

// looseEqual implements == with GitHub's coercion rules.
func looseEqual(a, b any) bool {
	ka, kb := kind(a), kind(b)
	if ka != kb {
		if ka == 4 || kb == 4 {
			return false
		}
		return toNumber(a) == toNumber(b)
	}
	switch ka {
	case 0:
		return true
	case 1:
		return a.(bool) == b.(bool)
	case 2:
		return a.(float64) == b.(float64)
	case 3:
		return strings.EqualFold(a.(string), b.(string))
	}
	// Objects and arrays compare by identity; values built separately are never equal.
	return false
}

func compare(a, b any) (int, bool) {
	ka, kb := kind(a), kind(b)
	if ka == 3 && kb == 3 {
		return strings.Compare(strings.ToLower(a.(string)), strings.ToLower(b.(string))), true
	}
	if ka == 4 || kb == 4 {
		return 0, false
	}
	x, y := toNumber(a), toNumber(b)
	if math.IsNaN(x) || math.IsNaN(y) {
		return 0, false
	}
	switch {
	case x < y:
		return -1, true
	case x > y:
		return 1, true
	}
	return 0, true
}

func status(c *Context, fn string) bool {
	if c == nil || c.Status == nil {
		return fn == "success" || fn == "always"
	}
	return c.Status(fn)
}

// ---- lexer ----

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tStr
	tIdent
	tOp // . [ ] ( ) , ! == != < <= > >= && || *
)

type token struct {
	kind tokKind
	text string
	num  float64
}

type parser struct {
	src  string
	toks []token
	pos  int
}

func (p *parser) lex() error {
	s := p.src
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(s) {
					return fmt.Errorf("unterminated string")
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(s[j])
				j++
			}
			p.toks = append(p.toks, token{kind: tStr, text: b.String()})
			i = j + 1
		case c >= '0' && c <= '9' || (c == '-' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' && p.prevAllowsSign()):
			j := i + 1
			for j < len(s) && (isIdentChar(s[j]) || s[j] == '.' || ((s[j] == '-' || s[j] == '+') && (s[j-1] == 'e' || s[j-1] == 'E'))) {
				j++
			}
			text := s[i:j]
			f := toNumber(text)
			if math.IsNaN(f) {
				return fmt.Errorf("invalid number %q", text)
			}
			p.toks = append(p.toks, token{kind: tNum, text: text, num: f})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && (isIdentChar(s[j]) || s[j] == '-') {
				j++
			}
			p.toks = append(p.toks, token{kind: tIdent, text: s[i:j]})
			i = j
		default:
			two := ""
			if i+1 < len(s) {
				two = s[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||":
				p.toks = append(p.toks, token{kind: tOp, text: two})
				i += 2
				continue
			}
			if strings.IndexByte(".[](),!<>*", c) < 0 {
				return fmt.Errorf("unexpected character %q", string(c))
			}
			p.toks = append(p.toks, token{kind: tOp, text: string(c)})
			i++
		}
	}
	p.toks = append(p.toks, token{kind: tEOF})
	return nil
}

// prevAllowsSign reports whether a '-' here starts a negative number literal.
func (p *parser) prevAllowsSign() bool {
	if len(p.toks) == 0 {
		return true
	}
	t := p.toks[len(p.toks)-1]
	return t.kind == tOp && t.text != ")" && t.text != "]"
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentChar(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) accept(op string) bool {
	if t := p.peek(); t.kind == tOp && t.text == op {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expect(op string) error {
	if !p.accept(op) {
		t := p.peek()
		if t.kind == tEOF {
			return fmt.Errorf("expected %q at end of expression", op)
		}
		return fmt.Errorf("expected %q, got %q", op, t.text)
	}
	return nil
}

// ---- parser (precedence climbing) ----

var binPrec = map[string]int{"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4}

func (p *parser) expr(minPrec int) (node, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		prec, ok := binPrec[t.text]
		if t.kind != tOp || !ok || prec < minPrec {
			return left, nil
		}
		p.next()
		right, err := p.expr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &binary{op: t.text, l: left, r: right}
	}
}

func (p *parser) unary() (node, error) {
	if p.accept("!") {
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &not{n}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (node, error) {
	n, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.accept("."):
			t := p.next()
			switch {
			case t.kind == tIdent:
				n = &index{obj: n, key: &literal{t.text}}
			case t.kind == tOp && t.text == "*":
				n = &star{obj: n}
			default:
				return nil, fmt.Errorf("expected a property name after '.'")
			}
		case p.accept("["):
			if p.accept("*") {
				n = &star{obj: n}
			} else {
				k, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				n = &index{obj: n, key: k}
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
		default:
			return n, nil
		}
	}
}

func (p *parser) primary() (node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return &literal{t.num}, nil
	case tStr:
		return &literal{t.text}, nil
	case tIdent:
		switch t.text {
		case "true":
			return &literal{true}, nil
		case "false":
			return &literal{false}, nil
		case "null":
			return &literal{nil}, nil
		case "NaN":
			return &literal{math.NaN()}, nil
		case "Infinity":
			return &literal{math.Inf(1)}, nil
		}
		if p.accept("(") {
			name := strings.ToLower(t.text)
			spec, ok := functions[name]
			if !ok {
				return nil, fmt.Errorf("unknown function %s()", t.text)
			}
			var args []node
			if !p.accept(")") {
				for {
					a, err := p.expr(0)
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.accept(")") {
						break
					}
					if err := p.expect(","); err != nil {
						return nil, err
					}
				}
			}
			if len(args) < spec.min || (spec.max >= 0 && len(args) > spec.max) {
				return nil, fmt.Errorf("%s() takes %s argument(s), got %d", t.text, spec.arity(), len(args))
			}
			return &call{name: name, args: args}, nil
		}
		return &ident{t.text}, nil
	case tOp:
		if t.text == "(" {
			n, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			return n, p.expect(")")
		}
		return nil, fmt.Errorf("unexpected %q", t.text)
	}
	return nil, fmt.Errorf("unexpected end of expression")
}

// ---- AST ----

type evalErr string

type node interface{ eval(c *Context) any }

type literal struct{ v any }
type ident struct{ name string }
type index struct{ obj, key node }
type star struct{ obj node }
type not struct{ n node }
type binary struct {
	op   string
	l, r node
}
type call struct {
	name string
	args []node
}

// filtered marks the result of a .* filter so that a following property
// access maps over its elements.
type filtered []any

func (n *literal) eval(*Context) any { return n.v }

func (n *ident) eval(c *Context) any {
	if c == nil || c.Contexts == nil {
		return nil
	}
	if v, ok := c.Contexts[n.name]; ok {
		return v
	}
	for k, v := range c.Contexts {
		if strings.EqualFold(k, n.name) {
			return v
		}
	}
	return nil
}

func lookup(obj, key any) any {
	switch o := obj.(type) {
	case map[string]any:
		k, ok := key.(string)
		if !ok {
			k = String(key)
		}
		if v, ok := o[k]; ok {
			return v
		}
		for mk, v := range o {
			if strings.EqualFold(mk, k) {
				return v
			}
		}
	case []any:
		f := toNumber(key)
		if f != math.Trunc(f) || f < 0 || int(f) >= len(o) {
			return nil
		}
		return o[int(f)]
	}
	return nil
}

func (n *index) eval(c *Context) any {
	obj := n.obj.eval(c)
	key := n.key.eval(c)
	if f, ok := obj.(filtered); ok {
		out := filtered{}
		for _, e := range f {
			if v := lookup(e, key); v != nil {
				out = append(out, v)
			}
		}
		return out
	}
	return lookup(obj, key)
}

func (n *star) eval(c *Context) any {
	obj := n.obj.eval(c)
	out := filtered{}
	switch o := obj.(type) {
	case map[string]any:
		keys := make([]string, 0, len(o))
		for k := range o {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, o[k])
		}
	case []any:
		out = append(out, o...)
	case filtered:
		for _, e := range o {
			switch ee := e.(type) {
			case map[string]any:
				for _, v := range ee {
					out = append(out, v)
				}
			case []any:
				out = append(out, ee...)
			}
		}
	}
	return out
}

func (n *not) eval(c *Context) any { return !Truthy(unfilter(n.n.eval(c))) }

func unfilter(v any) any {
	if f, ok := v.(filtered); ok {
		return []any(f)
	}
	return v
}

func (n *binary) eval(c *Context) any {
	switch n.op {
	case "&&":
		l := unfilter(n.l.eval(c))
		if !Truthy(l) {
			return l
		}
		return unfilter(n.r.eval(c))
	case "||":
		l := unfilter(n.l.eval(c))
		if Truthy(l) {
			return l
		}
		return unfilter(n.r.eval(c))
	}
	l, r := unfilter(n.l.eval(c)), unfilter(n.r.eval(c))
	switch n.op {
	case "==":
		return looseEqual(l, r)
	case "!=":
		return !looseEqual(l, r)
	}
	cmp, ok := compare(l, r)
	if !ok {
		return false
	}
	switch n.op {
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	}
	panic(evalErr("unknown operator " + n.op))
}

func (n *call) eval(c *Context) any {
	args := make([]any, len(n.args))
	for i, a := range n.args {
		args[i] = unfilter(a.eval(c))
	}
	return functions[n.name].fn(c, args)
}

func walk(n node, f func(node)) {
	f(n)
	switch x := n.(type) {
	case *index:
		walk(x.obj, f)
		walk(x.key, f)
	case *star:
		walk(x.obj, f)
	case *not:
		walk(x.n, f)
	case *binary:
		walk(x.l, f)
		walk(x.r, f)
	case *call:
		for _, a := range x.args {
			walk(a, f)
		}
	}
}

func usesStatus(n node) bool {
	found := false
	walk(n, func(n node) {
		if c, ok := n.(*call); ok {
			switch c.name {
			case "success", "failure", "cancelled", "always":
				found = true
			}
		}
	})
	return found
}

// ---- functions ----

type fnSpec struct {
	min, max int
	fn       func(c *Context, args []any) any
}

func (f fnSpec) arity() string {
	switch {
	case f.max < 0:
		return fmt.Sprintf("at least %d", f.min)
	case f.min == f.max:
		return strconv.Itoa(f.min)
	}
	return fmt.Sprintf("%d to %d", f.min, f.max)
}

var functions map[string]fnSpec

func init() {
	statusFn := func(name string) fnSpec {
		return fnSpec{0, 0, func(c *Context, _ []any) any { return status(c, name) }}
	}
	functions = map[string]fnSpec{
		"success":   statusFn("success"),
		"failure":   statusFn("failure"),
		"cancelled": statusFn("cancelled"),
		"always":    statusFn("always"),
		"hashfiles": {1, -1, func(c *Context, a []any) any {
			if c == nil || c.HashFiles == nil {
				return ""
			}
			pats := make([]string, len(a))
			for i, v := range a {
				pats[i] = String(v)
			}
			return c.HashFiles(pats)
		}},
		"contains": {2, 2, func(_ *Context, a []any) any {
			if arr, ok := a[0].([]any); ok {
				for _, e := range arr {
					if looseEqual(e, a[1]) {
						return true
					}
				}
				return false
			}
			return strings.Contains(strings.ToLower(String(a[0])), strings.ToLower(String(a[1])))
		}},
		"startswith": {2, 2, func(_ *Context, a []any) any {
			return strings.HasPrefix(strings.ToLower(String(a[0])), strings.ToLower(String(a[1])))
		}},
		"endswith": {2, 2, func(_ *Context, a []any) any {
			return strings.HasSuffix(strings.ToLower(String(a[0])), strings.ToLower(String(a[1])))
		}},
		"format": {1, -1, func(_ *Context, a []any) any { return format(String(a[0]), a[1:]) }},
		"join": {1, 2, func(_ *Context, a []any) any {
			sep := ","
			if len(a) > 1 {
				sep = String(a[1])
			}
			arr, ok := a[0].([]any)
			if !ok {
				return String(a[0])
			}
			parts := make([]string, len(arr))
			for i, e := range arr {
				parts[i] = String(e)
			}
			return strings.Join(parts, sep)
		}},
		"tojson": {1, 1, func(_ *Context, a []any) any {
			b, _ := json.MarshalIndent(normalize(a[0]), "", "  ")
			return string(b)
		}},
		"fromjson": {1, 1, func(_ *Context, a []any) any {
			var v any
			if err := json.Unmarshal([]byte(String(a[0])), &v); err != nil {
				panic(evalErr("fromJSON: " + err.Error()))
			}
			return v
		}},
	}
}

// format implements format('{0} {1}', a, b) with {{ and }} escapes.
func format(f string, args []any) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		switch {
		case f[i] == '{' && i+1 < len(f) && f[i+1] == '{':
			b.WriteByte('{')
			i++
		case f[i] == '}' && i+1 < len(f) && f[i+1] == '}':
			b.WriteByte('}')
			i++
		case f[i] == '{':
			j := strings.IndexByte(f[i:], '}')
			if j < 0 {
				panic(evalErr("format: unclosed {"))
			}
			n, err := strconv.Atoi(f[i+1 : i+j])
			if err != nil || n < 0 || n >= len(args) {
				panic(evalErr(fmt.Sprintf("format: bad placeholder %s", f[i:i+j+1])))
			}
			b.WriteString(String(args[n]))
			i += j
		default:
			b.WriteByte(f[i])
		}
	}
	return b.String()
}
