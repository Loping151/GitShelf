package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// ---- image ----

type imageRenderer struct{}

func (imageRenderer) Name() string { return "image" }
func (imageRenderer) CanRender(r Request) bool {
	return imageExts[r.File.Ext]
}
func (imageRenderer) Render(r Request) (Result, error) {
	// SVG is served through raw as text/plain (sanitized at source); for inline
	// display we still use <img> which treats it as an image, not a document.
	img := fmt.Sprintf(`<div class="preview-image"><img src="%s" alt="%s" loading="lazy"></div>`,
		html.EscapeString(r.RawURL), html.EscapeString(r.File.Path))
	return Result{Kind: "image", HTML: template.HTML(img)}, nil
}

// ---- pdf ----

type pdfRenderer struct{}

func (pdfRenderer) Name() string { return "pdf" }
func (pdfRenderer) CanRender(r Request) bool {
	return r.File.Ext == ".pdf"
}
func (pdfRenderer) Render(r Request) (Result, error) {
	raw := html.EscapeString(r.RawURL)
	var b strings.Builder
	if r.PDFEngine == "pdfjs" {
		// pdf.js viewer shipped as a static asset; falls back to native if absent.
		fmt.Fprintf(&b, `<div class="preview-pdf"><iframe class="pdfjs" src="../../../static/pdfjs/viewer.html?file=%s" title="PDF preview"></iframe></div>`, raw)
	} else {
		fmt.Fprintf(&b, `<div class="preview-pdf"><object data="%s" type="application/pdf"><iframe src="%s" title="PDF preview"></iframe></object></div>`, raw, raw)
	}
	return Result{Kind: "pdf", HTML: template.HTML(b.String())}, nil
}

// ---- audio / video ----

type mediaRenderer struct{}

func (mediaRenderer) Name() string { return "media" }
func (mediaRenderer) CanRender(r Request) bool {
	return audioExts[r.File.Ext] || videoExts[r.File.Ext]
}
func (mediaRenderer) Render(r Request) (Result, error) {
	raw := html.EscapeString(r.RawURL)
	var el string
	if audioExts[r.File.Ext] {
		el = fmt.Sprintf(`<div class="preview-media"><audio controls preload="metadata" src="%s"></audio></div>`, raw)
	} else {
		el = fmt.Sprintf(`<div class="preview-media"><video controls preload="metadata" src="%s"></video></div>`, raw)
	}
	return Result{Kind: "media", HTML: template.HTML(el)}, nil
}

// ---- csv / tsv ----

type csvRenderer struct{}

func (csvRenderer) Name() string { return "csv" }
func (csvRenderer) CanRender(r Request) bool {
	return csvExts[r.File.Ext] && !r.IsBinary
}
func (csvRenderer) Render(r Request) (Result, error) {
	reader := csv.NewReader(bytes.NewReader(r.Content))
	reader.FieldsPerRecord = -1
	if r.File.Ext == ".tsv" {
		reader.Comma = '\t'
	}
	var b strings.Builder
	b.WriteString(`<div class="preview-csv"><table class="csv-table">`)
	rowCount := 0
	const maxRows = 5000
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// fall back to plain text on malformed CSV
			return (textRenderer{}).Render(r)
		}
		tag := "td"
		if rowCount == 0 {
			tag = "th"
			b.WriteString("<thead>")
		} else if rowCount == 1 {
			b.WriteString("<tbody>")
		}
		b.WriteString("<tr>")
		for _, cell := range rec {
			fmt.Fprintf(&b, "<%s>%s</%s>", tag, html.EscapeString(cell), tag)
		}
		b.WriteString("</tr>")
		if rowCount == 0 {
			b.WriteString("</thead>")
		}
		rowCount++
		if rowCount >= maxRows {
			break
		}
	}
	if rowCount > 1 {
		b.WriteString("</tbody>")
	}
	b.WriteString("</table>")
	if r.Truncated || rowCount >= maxRows {
		b.WriteString(`<p class="truncated">Table truncated for display.</p>`)
	}
	b.WriteString("</div>")
	return Result{Kind: "csv", HTML: template.HTML(b.String())}, nil
}

// ---- json ----

type jsonRenderer struct{}

func (jsonRenderer) Name() string { return "json" }
func (jsonRenderer) CanRender(r Request) bool {
	return r.File.Ext == ".json" && !r.IsBinary
}
func (jsonRenderer) Render(r Request) (Result, error) {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, r.Content, "", "  "); err != nil {
		// invalid JSON: show as code
		return (codeRenderer{}).Render(r)
	}
	res, err := highlight(pretty.Bytes(), "JSON", "json")
	res.Kind = "json"
	return res, err
}

// ---- code ----

type codeRenderer struct{}

func (codeRenderer) Name() string { return "code" }
func (codeRenderer) CanRender(r Request) bool {
	return !r.IsBinary // generic text/code fallback
}
func (codeRenderer) Render(r Request) (Result, error) {
	lexer := lexers.Match(r.File.Path)
	if lexer == nil {
		lexer = lexers.Analyse(string(r.Content))
	}
	name := "text"
	if lexer != nil {
		name = lexer.Config().Name
	}
	return highlight(r.Content, name, lexerKey(name))
}

func lexerKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// highlight runs Chroma with class-based output (themed via CSS).
func highlight(content []byte, langName, langKey string) (Result, error) {
	lexer := lexers.Get(langKey)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(
		chromahtml.WithClasses(true),
		chromahtml.WithLineNumbers(true),
		chromahtml.LineNumbersInTable(true),
		chromahtml.WithLinkableLineNumbers(true, "L"),
	)
	it, err := lexer.Tokenise(nil, string(content))
	if err != nil {
		return Result{}, err
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, it); err != nil {
		return Result{}, err
	}
	lines := bytes.Count(content, []byte{'\n'}) + 1
	return Result{Kind: "code", HTML: template.HTML(buf.String()), Language: langName, Lines: lines}, nil
}

// ---- plain text ----

type textRenderer struct{}

func (textRenderer) Name() string { return "text" }
func (textRenderer) CanRender(r Request) bool {
	return !r.IsBinary
}
func (textRenderer) Render(r Request) (Result, error) {
	esc := html.EscapeString(string(r.Content))
	out := fmt.Sprintf(`<pre class="plain-text"><code>%s</code></pre>`, esc)
	lines := bytes.Count(r.Content, []byte{'\n'}) + 1
	return Result{Kind: "text", HTML: template.HTML(out), Lines: lines}, nil
}

// ---- binary fallback ----

type binaryRenderer struct{}

func (binaryRenderer) Name() string             { return "binary" }
func (binaryRenderer) CanRender(r Request) bool { return true }
func (binaryRenderer) Render(r Request) (Result, error) {
	msg := fmt.Sprintf(
		`<div class="preview-binary"><p>Binary file (%d bytes) — no preview available.</p><a class="btn" href="%s" download>Download</a></div>`,
		r.File.Size, html.EscapeString(r.RawURL))
	return Result{Kind: "binary", HTML: template.HTML(msg)}, nil
}

// ChromaCSS returns the highlight stylesheet for a named Chroma style under the
// given CSS selector prefix (e.g. ".chroma").
func ChromaCSS(styleName, prefix string) string {
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf bytes.Buffer
	_ = formatter.WriteCSS(&buf, style)
	out := buf.String()
	if prefix != "" && prefix != ".chroma" {
		out = strings.ReplaceAll(out, ".chroma", prefix)
	}
	return out
}
