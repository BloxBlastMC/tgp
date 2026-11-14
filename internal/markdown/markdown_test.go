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
