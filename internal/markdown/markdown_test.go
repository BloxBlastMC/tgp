package markdown

import (
	"strings"
	"testing"
)

func TestRenderSanitizesAndRewrites(t *testing.T) {
	src := "# Title\n\n<script>alert(1)</script>\n\n[docs](guide.md#setup) [abs](https://example.com) ![logo](../img/logo.png)\n\n" +
		"- [x] done\n\n```go\nfunc main() {}\n```\n\n<a href=\"javascript:alert(1)\">x</a>\n"
	html, err := Render([]byte(src), RepoContext{Repo: "game", Ref: "main", Dir: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "javascript:"} {
		if strings.Contains(html, bad) {
			t.Errorf("output contains %q:\n%s", bad, html)
		}
	}
	for _, want := range []string{
		`<h1 id="title">Title</h1>`,
		`href="/game/blob?path=docs%2Fguide.md&amp;ref=main#setup"`,
		`src="/api/v1/repos/game/raw?path=img%2Flogo.png&amp;ref=main"`,
		`href="https://example.com"`,
		`class="language-go"`,
		`type="checkbox"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

func TestNoRepoContextLeavesLinks(t *testing.T) {
	html, _ := Render([]byte("[a](b.md)"), RepoContext{})
	if !strings.Contains(html, `href="b.md"`) {
		t.Fatal(html)
	}
}

func TestIssueRefs(t *testing.T) {
	rc := RepoContext{Repo: "game", Ref: "main"}
	cases := map[string][]string{
		"Fixes #12 and #3.":           {`<a href="/game/issues/12" rel="nofollow">#12</a>`, `<a href="/game/issues/3" rel="nofollow">#3</a>`},
		"#7 at start":                 {`<a href="/game/issues/7" rel="nofollow">#7</a> at start`},
		"`#5` in code":                {`<code>#5</code>`},
		"[see #9](https://x.test/)":   {`>see #9</a>`},
		"a#4 and &#39; and x/#2 here": {`a#4`},
	}
	for in, wants := range cases {
		out, err := Render([]byte(in), rc)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wants {
			if !strings.Contains(out, w) {
				t.Errorf("Render(%q) = %q, want %q", in, out, w)
			}
		}
	}
	if out, _ := Render([]byte("`#5` and [#9](https://x.test/) and a#4"), rc); strings.Contains(out, "/issues/") {
		t.Errorf("refs linked in code/links/words: %s", out)
	}
	if out, _ := Render([]byte("#3"), RepoContext{}); strings.Contains(out, "<a") {
		t.Error("linked without a repo")
	}
}

func TestImagesInsideLinksResolve(t *testing.T) {
	rc := RepoContext{Repo: "game", Ref: "main", Dir: "docs"}
	out, err := Render([]byte("[![logo](img/logo.png)](https://x.test/) and [![b](../b.svg)](guide.md) and ![alt](c.png) #5"), rc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<a href="https://x.test/" rel="nofollow noopener" target="_blank"><img src="/api/v1/repos/game/raw?path=docs%2Fimg%2Flogo.png&amp;ref=main" alt="logo"></a>`,
		`<a href="/game/blob?path=docs%2Fguide.md&amp;ref=main" rel="nofollow"><img src="/api/v1/repos/game/raw?path=b.svg&amp;ref=main" alt="b"></a>`,
		`<img src="/api/v1/repos/game/raw?path=docs%2Fc.png&amp;ref=main" alt="alt">`,
		`<a href="/game/issues/5" rel="nofollow">#5</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
}
