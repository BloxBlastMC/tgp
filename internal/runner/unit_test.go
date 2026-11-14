package runner

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	got := parseEnvFile("A=1\nB=x=y\n\nMULTI<<EOF\nline 1\nline 2\nEOF\nC=\n")
	want := map[string]string{"A": "1", "B": "x=y", "MULTI": "line 1\nline 2", "C": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%#v", got)
	}
}

func TestMaskerAndLines(t *testing.T) {
	m := &masker{}
	m.add("hunter22")
	m.add("ab") // too short to mask
	m.add("line-one\nline-two")
	if got := m.mask("pw=hunter22 x=ab line-two"); got != "pw=*** x=ab ***" {
		t.Fatalf("%q", got)
	}
	ls := &logStream{mask: m, first: 1}
	w := &lineWriter{step: 2, out: ls}
	_, _ = w.Write([]byte("progress 10%\rprogress 100%\nhello "))
	_, _ = w.Write([]byte("hunter22\n::add-mask::topsecret\nsaid topsecret\npartial"))
	w.Flush()
	var texts []string
	for _, l := range ls.pending {
		if l.Step != 2 {
			t.Fatalf("step %d", l.Step)
		}
		texts = append(texts, l.Text)
	}
	want := "progress 100%|hello ***|::add-mask::***|said ***|partial"
	if strings.Join(texts, "|") != want {
		t.Fatalf("%q", strings.Join(texts, "|"))
	}
}

func TestShellCommand(t *testing.T) {
	cases := map[string]string{
		"bash":            "bash --noprofile --norc -eo pipefail /s",
		"sh":              "sh -e /s",
		"python":          "python /s",
		"perl {0} --flag": "perl /s --flag",
	}
	for shell, want := range cases {
		cmd, _ := shellCommand(shell, "/s")
		if got := strings.Join(cmd, " "); got != want {
			t.Errorf("%s: %q", shell, got)
		}
	}
}

func TestImageTags(t *testing.T) {
	for img, want := range map[string]string{
		"debian:bookworm": "debian:bookworm", "ghcr.io/org/img:1.2": "img:1.2", "localhost:5000/img": "img",
		"img@sha256:abc": "img:digest",
	} {
		if got := lastSegment(img); got != want {
			t.Errorf("%s: %s", img, got)
		}
	}
}
