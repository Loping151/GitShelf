package render

import (
	"bytes"
	"html/template"
	"path"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

var mdExts = set(".md", ".markdown", ".mdown", ".mkd")

type markdownRenderer struct{}

func (markdownRenderer) Name() string { return "markdown" }
func (markdownRenderer) CanRender(r Request) bool {
	return mdExts[r.File.Ext] && !r.IsBinary
}
func (markdownRenderer) Render(r Request) (Result, error) {
	out := RenderMarkdownRel(r.Content, MDOptions{RawBase: r.RawBase, SrcBase: r.SrcBase, Dir: r.Dir})
	return Result{Kind: "markdown", HTML: out}, nil
}

var (
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM, // tables, strikethrough, autolink, tasklist
			extension.Footnote,
			extension.DefinitionList,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(), // anchorable headings
		),
		goldmark.WithRendererOptions(
			gmhtml.WithHardWraps(),
			// Allow inline/raw HTML through (GitHub-like). This is safe because
			// bluemonday is the real security boundary: RenderMarkdown sanitizes
			// the rendered output against a strict allow-list afterward, so any
			// <script>/<iframe>/on*=… is stripped regardless.
			gmhtml.WithUnsafe(),
		),
	)
	mdPolicy = newMarkdownPolicy()
)

// MDOptions carries per-document context so relative image/link URLs can be
// resolved to the repo's raw/src endpoints (GitHub does the same). Dir is the
// directory of the source document within the repo (e.g. "" for a root README,
// "docs" for docs/README.md).
type MDOptions struct {
	RawBase string // e.g. "/<repo>/raw/<ref>"  — relative <img src> resolve here
	SrcBase string // e.g. "/<repo>/src/<ref>"  — relative <a href> resolve here
	Dir     string // directory of the document within the repo
}

// RenderMarkdown converts GFM markdown to sanitized HTML.
func RenderMarkdown(src []byte) template.HTML {
	return renderMarkdown(src, MDOptions{})
}

// RenderMarkdownRel is like RenderMarkdown but rewrites relative image/link
// URLs against the given repo/ref bases so they load from the raw/src routes.
func RenderMarkdownRel(src []byte, o MDOptions) template.HTML {
	return renderMarkdown(src, o)
}

func renderMarkdown(src []byte, o MDOptions) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		// On failure, fall back to an escaped <pre>.
		return template.HTML("<pre>" + template.HTMLEscapeString(string(src)) + "</pre>")
	}
	out := buf.Bytes()
	if o.RawBase != "" || o.SrcBase != "" {
		out = rewriteRelativeURLs(out, o)
	}
	clean := mdPolicy.SanitizeBytes(out)
	return template.HTML(clean)
}

// rewriteRelativeURLs rewrites relative <img src> and <a href> destinations to
// absolute repo routes, leaving absolute/anchor/scheme URLs untouched. Runs
// before sanitization; only the two target tags are re-serialized, everything
// else is passed through verbatim.
func rewriteRelativeURLs(b []byte, o MDOptions) []byte {
	z := html.NewTokenizer(bytes.NewReader(b))
	var out bytes.Buffer
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out.Bytes() // includes EOF
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			raw := append([]byte(nil), z.Raw()...)
			tok := z.Token()
			base := ""
			attrKey := ""
			switch tok.Data {
			case "img":
				base, attrKey = o.RawBase, "src"
			case "a":
				base, attrKey = o.SrcBase, "href"
			}
			if base != "" {
				changed := false
				for i := range tok.Attr {
					if tok.Attr[i].Key == attrKey {
						if nv := resolveRelURL(base, o.Dir, tok.Attr[i].Val); nv != tok.Attr[i].Val {
							tok.Attr[i].Val = nv
							changed = true
						}
					}
				}
				if changed {
					out.WriteString(tok.String())
					continue
				}
			}
			out.Write(raw)
			continue
		}
		out.Write(z.Raw())
	}
}

// resolveRelURL turns a repo-relative URL into base+"/"+cleanpath. Absolute,
// scheme, protocol-relative and pure-anchor URLs are returned unchanged.
func resolveRelURL(base, dir, val string) string {
	if val == "" || val[0] == '#' {
		return val
	}
	lower := strings.ToLower(val)
	for _, p := range []string{"http://", "https://", "//", "data:", "mailto:", "tel:"} {
		if strings.HasPrefix(lower, p) {
			return val
		}
	}
	// preserve any ?query / #fragment
	target, suffix := val, ""
	if i := strings.IndexAny(val, "?#"); i >= 0 {
		target, suffix = val[:i], val[i:]
	}
	var rel string
	if strings.HasPrefix(target, "/") {
		rel = strings.TrimPrefix(target, "/") // repo-root relative
	} else {
		rel = path.Join(dir, target)
	}
	rel = path.Clean(rel)
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") {
		return val
	}
	return base + "/" + rel + suffix
}

// newMarkdownPolicy builds a strict allow-list policy: GitHub-flavored content
// with anchors, tables and task lists, but no scripts, styles, iframes or event
// handlers.
func newMarkdownPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// heading anchors + task-list checkboxes produced by goldmark
	p.AllowAttrs("id").Globally()
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^[a-zA-Z0-9 _-]+$`)).Globally()
	p.AllowAttrs("type", "checked", "disabled").OnElements("input")
	p.AllowElements("input") // task list checkboxes (disabled)
	// tables + alignment
	p.AllowAttrs("align").OnElements("td", "th", "div", "p", "img", "table")
	// collapsible sections and other safe inline HTML common in READMEs
	p.AllowElements("details", "summary", "kbd", "samp", "var", "sub", "sup",
		"abbr", "dl", "dt", "dd", "picture", "source", "figure", "figcaption")
	p.AllowAttrs("open").OnElements("details")
	p.AllowAttrs("srcset", "media", "type").OnElements("source")
	// images: http(s)/relative/the raw endpoint, with optional sizing
	p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	p.AllowStandardURLs()
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}

// FirstParagraph extracts a short plain-text snippet from markdown for listings.
func FirstParagraph(src []byte) string {
	s := string(src)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		return line
	}
	return ""
}
