// Package markdown renders GitHub-flavoured Markdown to sanitised HTML.
package markdown

import (
	"bytes"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// RepoContext lets relative links and images in a document resolve inside a
// repository. Zero value: links are left as written.
type RepoContext struct {
	Repo string // repository name
	Ref  string // branch, tag or commit
	Dir  string // directory of the document, e.g. "docs"
}

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[\w+#-]+$`)).OnElements("code")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^(|checked|disabled)$`)).OnElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|right|center)$`)).OnElements("td", "th")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

// Render converts markdown to safe HTML.
func Render(src []byte, rc RepoContext) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(&linkRewriter{rc: rc}, 100)),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return "", err
	}
	return policy.Sanitize(buf.String()), nil
}

type linkRewriter struct{ rc RepoContext }

func (l *linkRewriter) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	if l.rc.Repo == "" {
		return
	}
	var texts []*ast.Text
	// inLink counts enclosing links and images: their text isn't turned
	// into issue links, but images inside links (badges, logos) still need
	// their paths resolved, so their children are walked.
	inLink := 0
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.(type) {
		case *ast.Link, *ast.Image:
			if !entering {
				inLink--
			}
		}
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Text:
			if inLink == 0 {
				texts = append(texts, v)
			}
		}
		switch v := n.(type) {
		case *ast.Image:
			if p, ok := l.resolve(string(v.Destination)); ok {
				v.Destination = []byte("/api/v1/repos/" + url.PathEscape(l.rc.Repo) + "/raw?" + url.Values{"ref": {l.rc.Ref}, "path": {p}}.Encode())
			}
			inLink++
		case *ast.Link:
			dest := string(v.Destination)
			frag := ""
			if i := strings.IndexByte(dest, '#'); i > 0 {
				dest, frag = dest[:i], dest[i:]
			}
			if p, ok := l.resolve(dest); ok {
				v.Destination = []byte("/" + url.PathEscape(l.rc.Repo) + "/blob?" + url.Values{"ref": {l.rc.Ref}, "path": {p}}.Encode() + frag)
			}
			inLink++
		}
		switch n.(type) {
		case *ast.AutoLink, *ast.CodeSpan:
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, t := range texts {
		l.linkRefs(t, reader.Source())
	}
}

// issueRefRe matches "#123" not preceded by a word character or "&" (HTML entities).
var issueRefRe = regexp.MustCompile(`(?:^|[^\w&/#])#([0-9]{1,9})\b`)

// linkRefs turns "#123" in a text node into links to issue/PR pages.
func (l *linkRewriter) linkRefs(t *ast.Text, src []byte) {
	seg := t.Segment
	val := seg.Value(src)
	ms := issueRefRe.FindAllSubmatchIndex(val, -1)
	if len(ms) == 0 {
		return
	}
	parent := t.Parent()
	if parent == nil {
		return
	}
	pos := 0
	var last ast.Node = t
	insert := func(n ast.Node) {
		parent.InsertAfter(parent, last, n)
		last = n
	}
	for _, m := range ms {
		hash := m[2] - 1 // index of '#'
		if hash > pos {
			insert(ast.NewTextSegment(text.NewSegment(seg.Start+pos, seg.Start+hash)))
		}
		num := string(val[m[2]:m[3]])
		link := ast.NewLink()
		link.Destination = []byte("/" + url.PathEscape(l.rc.Repo) + "/issues/" + num)
		link.AppendChild(link, ast.NewTextSegment(text.NewSegment(seg.Start+hash, seg.Start+m[3])))
		insert(link)
		pos = m[3]
	}
	if pos < len(val) {
		rest := ast.NewTextSegment(text.NewSegment(seg.Start+pos, seg.Stop))
		rest.SetSoftLineBreak(t.SoftLineBreak())
		rest.SetHardLineBreak(t.HardLineBreak())
		insert(rest)
	}
	parent.RemoveChild(parent, t)
}

// resolve turns a relative destination into a repository path.
func (l *linkRewriter) resolve(dest string) (string, bool) {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "//") {
		return "", false
	}
	if u, err := url.Parse(dest); err != nil || u.Scheme != "" || u.Host != "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(dest); err == nil {
		dest = unescaped
	}
	var p string
	if strings.HasPrefix(dest, "/") {
		p = path.Clean(dest)
	} else {
		p = path.Clean("/" + path.Join(l.rc.Dir, dest))
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return "", false
	}
	return p, true
}
