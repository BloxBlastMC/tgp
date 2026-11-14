package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestRewriteJSONArgs(t *testing.T) {
	fields := []string{"name", "clone_url", "id"}
	cases := []struct{ in, want []string }{
		{[]string{"repo", "view", "--json", "name,id"}, []string{"repo", "view", "--json=name,id"}},
		{[]string{"repo", "view", "--json", "myrepo"}, []string{"repo", "view", "--json", "myrepo"}},
		{[]string{"repo", "view", "--json", "name", "x"}, []string{"repo", "view", "--json=name", "x"}},
		{[]string{"repo", "view", "--json"}, []string{"repo", "view", "--json"}},
		{[]string{"clone", "--", "--json", "name"}, []string{"clone", "--", "--json", "name"}},
	}
	for _, c := range cases {
		if got := rewriteJSONArgs(c.in, fields); !reflect.DeepEqual(got, c.want) {
			t.Errorf("rewriteJSONArgs(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := rewriteJSONArgs([]string{"--json", "name"}, nil); !reflect.DeepEqual(got, []string{"--json", "name"}) {
		t.Errorf("without known fields args must be untouched: %v", got)
	}
}

func TestSelectFields(t *testing.T) {
	data := []any{map[string]any{"a": 1, "b": 2}, map[string]any{"a": 3}}
	got, err := selectFields(data, []string{"a", "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"a": 1, "b": 2}, map[string]any{"a": 3, "b": nil}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if _, err := selectFields(data, []string{"zzz"}, []string{"a", "b"}); err == nil || toError(err).Exit != ExitUsage {
		t.Fatalf("unknown field: %v", err)
	}
}

func TestNormalizeHostURL(t *testing.T) {
	cases := map[string][2]string{
		"git.example.com":              {"https://git.example.com", "git.example.com"},
		"http://localhost:3000/":       {"http://localhost:3000", "localhost:3000"},
		"https://Git.Example.com/tgp/": {"https://Git.Example.com/tgp", "git.example.com"},
		"https://u:p@h.com/?x=1":       {"https://h.com", "h.com"},
	}
	for in, want := range cases {
		base, key, err := normalizeHostURL(in)
		if err != nil || base != want[0] || key != want[1] {
			t.Errorf("normalizeHostURL(%q) = %q, %q, %v; want %v", in, base, key, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "https://"} {
		if _, _, err := normalizeHostURL(bad); err == nil {
			t.Errorf("normalizeHostURL(%q) should fail", bad)
		}
	}
}

func TestSplitRemote(t *testing.T) {
	key, path, ok := splitRemote("https://alice@Git.Example.com:8443/game.git")
	if !ok || key != "git.example.com:8443" || path != "game.git" {
		t.Fatalf("got %q %q %v", key, path, ok)
	}
	if _, _, ok := splitRemote("git@github.com:x/y.git"); ok {
		t.Fatal("scp-style remotes aren't tgp remotes")
	}
}

func TestAPIErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   string
		exit   int
	}{
		{404, `{"error":{"code":"not_found","message":"nope"}}`, "not_found", ExitNotFound},
		{401, `{"error":{"code":"unauthenticated","message":"x"}}`, "unauthenticated", ExitAuth},
		{403, `{"error":{"code":"password_change_required","message":"x"}}`, "password_change_required", ExitAuth},
		{403, `{"error":{"code":"forbidden","message":"x"}}`, "forbidden", ExitForbidden},
		{422, `{"error":{"code":"validation_failed","message":"x"}}`, "validation_failed", ExitConflict},
		{503, `{"error":{"code":"service_unavailable","message":"busy"}}`, "service_unavailable", ExitUnavailable},
		{502, `<html>Bad gateway</html>`, "internal_error", ExitUnavailable},
		{404, `not json`, "not_found", ExitNotFound},
		{500, ``, "internal_error", ExitError},
	}
	for _, c := range cases {
		e := apiError(c.status, []byte(c.body))
		if e.Code != c.code || e.Exit != c.exit || e.Status != c.status {
			t.Errorf("apiError(%d, %q) = %+v; want code %s exit %d", c.status, c.body, e, c.code, c.exit)
		}
	}
}

func TestWriteError(t *testing.T) {
	var b bytes.Buffer
	writeError(&b, &Error{Code: "not_found", Message: "no repo", Hint: "try x", Status: 404}, true)
	if strings.TrimSpace(b.String()) != `{"error":{"code":"not_found","message":"no repo","hint":"try x","status":404}}` {
		t.Fatalf("json error: %s", b.String())
	}
	b.Reset()
	writeError(&b, &Error{Code: "x", Message: "boom", Hint: "do y"}, false)
	if b.String() != "error: boom\nhint: do y\n" {
		t.Fatalf("human error: %q", b.String())
	}
	b.Reset()
	writeError(&b, &Error{Code: "x", silent: true}, true)
	if b.Len() != 0 {
		t.Fatal("silent error printed")
	}
}

func TestToJSONSchema(t *testing.T) {
	var refs []string
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"at":   map[string]any{"type": "string", "nullable": true, "example": "x"},
			"role": map[string]any{"$ref": "#/components/schemas/Role"},
		},
	}
	got := toJSONSchema(in, func(r string) { refs = append(refs, r) }).(map[string]any)
	props := got["properties"].(map[string]any)
	at := props["at"].(map[string]any)
	if !reflect.DeepEqual(at["type"], []any{"string", "null"}) || at["nullable"] != nil || !reflect.DeepEqual(at["examples"], []any{"x"}) {
		t.Fatalf("at = %v", at)
	}
	if props["role"].(map[string]any)["$ref"] != "#/$defs/Role" || !reflect.DeepEqual(refs, []string{"Role"}) {
		t.Fatalf("role = %v, refs %v", props["role"], refs)
	}
}

func TestCLISchemasDontCollide(t *testing.T) {
	if _, err := spec(); err != nil {
		t.Fatal(err)
	}
}

func TestInsecureRemote(t *testing.T) {
	for in, want := range map[string]bool{
		"http://git.example.com": true, "https://git.example.com": false,
		"http://localhost:3000": false, "http://127.0.0.1:3000": false, "http://[::1]:3000": false, "http://10.0.0.5": true,
	} {
		if got := insecureRemote(in); got != want {
			t.Errorf("insecureRemote(%q) = %v", in, got)
		}
	}
}
