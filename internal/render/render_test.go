package render

import (
	"strings"
	"testing"
)

func TestDetectExt(t *testing.T) {
	cases := map[string]string{
		"a/b/main.go": ".go",
		"README.md":   ".md",
		"noext":       "",
		"dir/.hidden": "", // leading dot is not an extension
		"archive.TAR": ".tar",
	}
	for path, want := range cases {
		if got := Detect(path, 0).Ext; got != want {
			t.Errorf("Detect(%q).Ext = %q, want %q", path, got, want)
		}
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("plain text\nwith lines")) {
		t.Error("text classified as binary")
	}
	if !IsBinary([]byte{0x00, 0x01, 0x02}) {
		t.Error("NUL bytes not classified as binary")
	}
}

func TestMarkdownSanitizes(t *testing.T) {
	in := []byte("# Hi\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")
	out := string(RenderMarkdown(in))
	if strings.Contains(out, "<script>") {
		t.Error("script tag not stripped")
	}
	if strings.Contains(out, "javascript:") {
		t.Error("javascript: URL not stripped")
	}
	if !strings.Contains(out, "<table>") {
		t.Error("GFM table not rendered")
	}
	if !strings.Contains(out, "id=\"hi\"") {
		t.Error("heading anchor id missing")
	}
}

func TestMarkdownAllowsSafeHTML(t *testing.T) {
	in := []byte("Text with <kbd>Ctrl</kbd>.\n\n" +
		"<details><summary>More</summary>hidden <b>content</b></details>\n\n" +
		"<div align=\"center\"><img src=\"/x.png\" width=\"100\"></div>\n\n" +
		"<script>alert(1)</script><iframe src=\"evil\"></iframe>")
	out := string(RenderMarkdown(in))
	for _, want := range []string{"<kbd>", "<details>", "<summary>", "align=\"center\"", "width=\"100\""} {
		if !strings.Contains(out, want) {
			t.Errorf("safe HTML %q was stripped:\n%s", want, out)
		}
	}
	for _, bad := range []string{"<script", "<iframe"} {
		if strings.Contains(out, bad) {
			t.Errorf("dangerous HTML %q survived sanitization", bad)
		}
	}
}

func TestMarkdownRelativeURLRewrite(t *testing.T) {
	src := []byte("![icon](./ICON.png)\n\n" +
		"<img src=\"assets/footer.png\">\n\n" +
		"[docs](../guide/x.md)\n\n" +
		"![abs](https://example.com/a.png)\n\n" +
		"[anchor](#top) and [root](/logo.png)")
	o := MDOptions{RawBase: "/repo/raw/main", SrcBase: "/repo/src/main", Dir: "sub"}
	out := string(RenderMarkdownRel(src, o))
	checks := map[string]bool{
		`src="/repo/raw/main/sub/ICON.png"`:          true, // relative image, joined to Dir
		`src="/repo/raw/main/sub/assets/footer.png"`: true, // raw <img>
		`href="/repo/src/main/guide/x.md"`:           true, // ../ resolved above Dir
		`https://example.com/a.png`:                  true, // absolute untouched
		`href="#top"`:                                true, // anchor untouched
		`href="/repo/src/main/logo.png"`:             true, // root-relative
	}
	for want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// no rewrite when no bases given
	plain := string(RenderMarkdownRel([]byte("![x](./a.png)"), MDOptions{}))
	if !strings.Contains(plain, `src="./a.png"`) {
		t.Errorf("plain render should leave relative URL intact: %s", plain)
	}
}

func TestCodeRendererHighlights(t *testing.T) {
	reg := NewRegistry(nil, 1<<20)
	res, err := reg.Render(Request{
		File:    Detect("main.go", 0),
		Content: []byte("package main\nfunc main() {}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "code" {
		t.Fatalf("kind = %q, want code", res.Kind)
	}
	if !strings.Contains(string(res.HTML), "chroma") {
		t.Error("chroma output missing")
	}
}

func TestRegistryRoutesByKind(t *testing.T) {
	reg := NewRegistry(nil, 1<<20)
	cases := []struct {
		path string
		bin  bool
		kind string
	}{
		{"a.png", false, "image"},
		{"a.pdf", false, "pdf"},
		{"a.mp4", false, "media"},
		{"a.md", false, "markdown"},
		{"a.csv", false, "csv"},
		{"a.json", false, "json"},
		{"a.go", false, "code"},
		{"blob.bin", true, "binary"},
	}
	for _, c := range cases {
		content := []byte("name,x\n1,2\n")
		if c.path == "a.json" {
			content = []byte(`{"a":1}`)
		}
		res, err := reg.Render(Request{File: Detect(c.path, 10), Content: content, IsBinary: c.bin, RawURL: "/raw"})
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if res.Kind != c.kind {
			t.Errorf("%s: kind = %q, want %q", c.path, res.Kind, c.kind)
		}
	}
}

func TestDisableRenderer(t *testing.T) {
	reg := NewRegistry([]string{"csv"}, 1<<20)
	res, _ := reg.Render(Request{File: Detect("a.csv", 10), Content: []byte("a,b\n1,2\n")})
	if res.Kind == "csv" {
		t.Error("disabled csv renderer still used")
	}
}
