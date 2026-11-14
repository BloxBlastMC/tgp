package expr

import (
	"strings"
	"testing"
)

func ctx() *Context {
	return &Context{Contexts: map[string]any{
		"github": map[string]any{"ref": "refs/heads/main", "event_name": "push", "run_number": 7.0},
		"matrix": map[string]any{"os": "linux", "go": "1.27"},
		"env":    map[string]any{"EMPTY": "", "N": "3"},
		"needs": map[string]any{
			"build": map[string]any{"result": "success", "outputs": map[string]any{"version": "1.2.3"}},
			"lint":  map[string]any{"result": "failure", "outputs": map[string]any{}},
		},
		"list": []any{"a", "B", 3.0},
	}}
}

func TestEval(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"github.ref == 'refs/heads/main'", "true"},
		{"github.REF == 'REFS/HEADS/MAIN'", "true"},
		{"github['event_name']", "push"},
		{"github.run_number", "7"},
		{"github.run_number > 5 && 'yes' || 'no'", "yes"},
		{"github.run_number < 5 && 'yes' || 'no'", "no"},
		{"matrix.os != 'windows'", "true"},
		{"env.EMPTY || 'default'", "default"},
		{"env.N == 3", "true"},
		{"env.missing", ""},
		{"null == 0", "true"},
		{"'' == false", "true"},
		{"!env.EMPTY", "true"},
		{"needs.build.outputs.version", "1.2.3"},
		{"contains(needs.*.result, 'failure')", "true"},
		{"contains(needs.*.result, 'cancelled')", "false"},
		{"contains(list, 'b')", "true"},
		{"contains('Hello World', 'WORLD')", "true"},
		{"startsWith(github.ref, 'refs/heads/')", "true"},
		{"endsWith(github.ref, '/MAIN')", "true"},
		{"format('{0}-{1} {{x}}', matrix.os, 2)", "linux-2 {x}"},
		{"join(list, ', ')", "a, B, 3"},
		{"join(list)", "a,B,3"},
		{"fromJSON('{\"a\":[1,2]}').a[1]", "2"},
		{"toJSON(matrix.go)", `"1.27"`},
		{"1.5 >= 1.5", "true"},
		{"-1 < 0", "true"},
		{"0x10 == 16", "true"},
		{"'it''s'", "it's"},
		{"success()", "true"},
		{"failure()", "false"},
	}
	for _, c := range cases {
		v, err := Eval(c.in, ctx())
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got := String(v); got != c.want {
			t.Errorf("%s = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestErrors(t *testing.T) {
	for _, in := range []string{"", "a ==", "nope()", "contains('a')", "'unterminated", "a.", "(a", "a b", "format('{3}', 1)", "fromJSON('{')", "a = b"} {
		if _, err := Eval(in, ctx()); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func TestCondition(t *testing.T) {
	failed := &Context{Contexts: ctx().Contexts, Status: func(fn string) bool { return fn == "failure" || fn == "always" }}
	cases := []struct {
		cond string
		c    *Context
		want bool
	}{
		{"", ctx(), true},
		{"", failed, false},
		{"github.event_name == 'push'", ctx(), true},
		{"${{ github.event_name == 'push' }}", ctx(), true},
		{"github.event_name == 'push'", failed, false}, // implicit success() &&
		{"failure()", failed, true},
		{"always()", failed, true},
		{"failure() && matrix.os == 'linux'", failed, true},
	}
	for _, c := range cases {
		got, err := Condition(c.cond, c.c)
		if err != nil || got != c.want {
			t.Errorf("Condition(%q) = %v, %v; want %v", c.cond, got, err, c.want)
		}
	}
}

func TestInterpolate(t *testing.T) {
	got, err := Interpolate("go ${{ matrix.go }} on ${{ matrix.os }} ${{ '}}' }}!", ctx())
	if err != nil || got != "go 1.27 on linux }}!" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := Interpolate("${{ matrix.go ", ctx()); err == nil {
		t.Fatal("unterminated: expected error")
	}
	got, _ = Interpolate("${{ fromJSON('[1,2]') }}", ctx())
	if !strings.Contains(got, "1") || !strings.HasPrefix(got, "[") {
		t.Fatalf("array: %q", got)
	}
	names, err := Check("${{ secrets.TOKEN }} and ${{ matrix.os == env.X }}")
	if err != nil || strings.Join(names, ",") != "secrets,env,matrix" {
		t.Fatalf("check: %v %v", names, err)
	}
}
