// Package render is the pluggable file render pipeline. A RendererRegistry
// routes a blob (by extension / content sniff) to a Renderer that produces
// safe HTML for in-browser preview. Renderers can be disabled via config and
// new ones registered (the registry is the extension point).
package render

import (
	"html/template"
)

// Request carries everything a renderer needs.
type Request struct {
	File      FileInfo
	Content   []byte // may be nil for media (served via raw URL instead)
	Truncated bool   // content was cut at max_render_bytes
	RawURL    string // URL to fetch the raw bytes
	PDFEngine string // "pdfjs" | "native"
	IsBinary  bool

	// Repo context for resolving relative URLs in Markdown (optional).
	RawBase string // "/<repo>/raw/<ref>"
	SrcBase string // "/<repo>/src/<ref>"
	Dir     string // directory of this file within the repo
}

// Result is renderer output.
type Result struct {
	Kind     string        // code|markdown|image|pdf|media|csv|json|text|binary
	HTML     template.HTML // sanitized, ready to embed
	Language string        // for code
	Lines    int           // for code/text, enables line gutters client-side
}

// Renderer turns a Request into a Result.
type Renderer interface {
	Name() string
	// CanRender reports whether this renderer handles the request.
	CanRender(r Request) bool
	Render(r Request) (Result, error)
}

// Registry holds renderers in priority order and routes requests.
type Registry struct {
	renderers []Renderer
	disabled  map[string]bool
	maxBytes  int64
}

// NewRegistry builds the default registry honoring disabled names.
func NewRegistry(disabled []string, maxRenderBytes int64) *Registry {
	dis := make(map[string]bool, len(disabled))
	for _, d := range disabled {
		dis[d] = true
	}
	reg := &Registry{disabled: dis, maxBytes: maxRenderBytes}
	// Order matters: most specific first, binary/text last as fallbacks.
	reg.register(
		&imageRenderer{},
		&pdfRenderer{},
		&mediaRenderer{},
		&markdownRenderer{},
		&csvRenderer{},
		&jsonRenderer{},
		&codeRenderer{}, // catches most text via lexer match
		&textRenderer{}, // plain-text fallback
		&binaryRenderer{},
	)
	return reg
}

func (reg *Registry) register(rs ...Renderer) {
	for _, r := range rs {
		if reg.disabled[r.Name()] {
			continue
		}
		reg.renderers = append(reg.renderers, r)
	}
}

// MaxBytes is the configured render cap.
func (reg *Registry) MaxBytes() int64 { return reg.maxBytes }

// Render routes the request to the first matching renderer.
func (reg *Registry) Render(r Request) (Result, error) {
	for _, rn := range reg.renderers {
		if rn.CanRender(r) {
			return rn.Render(r)
		}
	}
	return (&binaryRenderer{}).Render(r)
}

// Names lists active renderer names (for diagnostics).
func (reg *Registry) Names() []string {
	out := make([]string, len(reg.renderers))
	for i, r := range reg.renderers {
		out[i] = r.Name()
	}
	return out
}
