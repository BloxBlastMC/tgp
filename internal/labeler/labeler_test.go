package labeler

import (
	"strings"
	"testing"
)

func TestGlobs(t *testing.T) {
	cases := []struct {
		glob string
		yes  []string
		no   []string
	}{
		{"**/*.md", []string{"README.md", "docs/a/b.md", ".github/x.md"}, []string{"README.mdx", "md"}},
		{"*.md", []string{"README.md", ".hidden.md"}, []string{"docs/a.md"}},
		{"services/issues/**", []string{"services/issues/main.go", "services/issues/a/b/c", "services/issues"}, []string{"services/issues2/x", "services/finance/x"}},
		{"src/?.go", []string{"src/a.go"}, []string{"src/ab.go", "src/.go"}},
		{"{api,web}/**/*.{ts,tsx}", []string{"api/x.ts", "web/src/App.tsx"}, []string{"cmd/x.ts", "web/x.go"}},
		{"docs/[ab]*.txt", []string{"docs/a1.txt", "docs/b.txt"}, []string{"docs/c.txt"}},
		{"docs/[!ab]*.txt", []string{"docs/c.txt"}, []string{"docs/a.txt"}},
		{"!src/**", []string{"docs/x"}, []string{"src/x"}},
		{"a/**/b", []string{"a/b", "a/x/b", "a/x/y/b"}, []string{"a/xb", "b"}},
		{"./Dockerfile", []string{"Dockerfile"}, []string{"x/Dockerfile"}},
	}
	for _, c := range cases {
		g, err := CompileGlob(c.glob)
		if err != nil {
			t.Fatalf("%s: %v", c.glob, err)
		}
		for _, p := range c.yes {
			if !g.Match(p) {
				t.Errorf("%s should match %s", c.glob, p)
			}
		}
		for _, p := range c.no {
			if g.Match(p) {
				t.Errorf("%s shouldn't match %s", c.glob, p)
			}
		}
	}
}

const v5 = `
issues:
  - changed-files:
      - any-glob-to-any-file: ['services/issues/**']
finance:
  - changed-files:
    - any-glob-to-any-file: services/finance/**
docs-only:
  - changed-files:
      - any-glob-to-all-files: ['**/*.md', 'docs/**']
docs-outside-src:
  - all:
      - changed-files:
          - any-glob-to-any-file: '**/*.md'
          - all-globs-to-all-files: '!src/**'
md-or-docs:
  - changed-files:
      - any-glob-to-any-file: '**/*.md'
  - changed-files:
      - all-globs-to-all-files: 'docs/**'
feature:
  - head-branch: ['^feature/', 'feat']
to-release:
  - base-branch: '^release/'
either:
  - any:
      - changed-files:
          - any-glob-to-any-file: 'web/**'
      - head-branch: '^ui/'
both:
  - all:
      - changed-files:
          - all-globs-to-any-file: ['services/**', '**/*.go']
      - base-branch: '^main$'
ci: '.tgp/**'
legacy: ['Makefile', 'scripts/*']
sync-labels: true
`

func TestMatch(t *testing.T) {
	c, err := Parse("labeler.yml", []byte(v5))
	if err != nil {
		t.Fatal(err)
	}
	if !c.SyncLabels || len(c.Labels) != 11 {
		t.Fatalf("config: sync=%v labels=%v", c.SyncLabels, c.Names())
	}
	cases := []struct {
		pr   PR
		want string
	}{
		{PR{Files: []string{"services/issues/main.go"}, HeadBranch: "fix", BaseBranch: "main"}, "both,issues"},
		{PR{Files: []string{"services/finance/a.txt", "README.md"}, HeadBranch: "feature/x", BaseBranch: "main"}, "docs-outside-src,feature,finance,md-or-docs"},
		{PR{Files: []string{"README.md", "docs/a.txt"}, HeadBranch: "x", BaseBranch: "release/1"}, "docs-outside-src,md-or-docs,to-release"},
		{PR{Files: []string{"README.md", "docs/b.md"}, HeadBranch: "x", BaseBranch: "main"}, "docs-only,docs-outside-src,md-or-docs"},
		{PR{Files: []string{"README.md", "src/x.go"}, HeadBranch: "x", BaseBranch: "main"}, "md-or-docs"},
		{PR{Files: []string{"docs/a.txt"}, HeadBranch: "x", BaseBranch: "main"}, "docs-only,md-or-docs"},
		{PR{Files: []string{"a.txt"}, HeadBranch: "ui/new", BaseBranch: "main"}, "either"},
		{PR{Files: []string{"web/App.tsx"}, HeadBranch: "x", BaseBranch: "main"}, "either"},
		{PR{Files: []string{".tgp/workflows/ci.yml", "scripts/build.sh"}, HeadBranch: "x", BaseBranch: "main"}, "ci,legacy"},
		{PR{Files: nil, HeadBranch: "feat-1", BaseBranch: "main"}, "feature"},
	}
	for _, tc := range cases {
		if got := strings.Join(c.Match(tc.pr), ","); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.pr, got, tc.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	bad := map[string]string{
		"x:\n  - changed-files:\n      - any-file: a\n":                  `unknown option "any-file"`,
		"x:\n  - branch: a\n":                                            `unknown key "branch"`,
		"x:\n  - head-branch: '('\n":                                     "invalid regular expression",
		"x:\n  - changed-files:\n      - any-glob-to-any-file: 'a/[b'\n": "",
		"- a\n":            "must map label names",
		"x: [\n":           "invalid YAML",
		"x: {a: 1}\n":      "rules must be a list",
		"x:\n  - any: a\n": "must be a list",
	}
	for src, want := range bad {
		_, err := Parse("l.yml", []byte(src))
		if want == "" {
			continue // accepted: an unclosed [ is literal
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", src, err, want)
		}
	}
	_, err := Parse("l.yml", []byte("ok: '*'\nx:\n  - branch: a\n"))
	if es, ok := err.(Errors); !ok || es[0].Line != 3 {
		t.Fatalf("position: %#v", err)
	}
	if c, err := Parse("l.yml", []byte("")); err != nil || len(c.Labels) != 0 {
		t.Fatalf("empty: %v", err)
	}
}
