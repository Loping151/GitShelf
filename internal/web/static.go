package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Loping151/gitshelf/internal/render"
)

//go:embed all:static
var staticFS embed.FS

// registerStatic wires the /static/ handler: embedded files plus a synthesized
// highlight.css generated from the Chroma styles (light + dark).
func (s *Server) registerStatic(mux *http.ServeMux) {
	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(sub))

	highlightCSS := buildHighlightCSS()

	mux.HandleFunc("GET "+s.path("/static/"), func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, s.path("/static/"))
		if rel == "highlight.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			_, _ = w.Write([]byte(highlightCSS))
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.StripPrefix(s.path("/static/"), fileServer).ServeHTTP(w, r)
	})
}

// buildHighlightCSS composes Chroma's class-based CSS for light and dark themes.
// Dark rules are scoped under [data-theme="dark"] so the page can toggle.
func buildHighlightCSS() string {
	var b strings.Builder
	b.WriteString("/* generated: chroma highlight themes */\n")
	b.WriteString(render.ChromaCSS("github", ".chroma"))
	b.WriteString("\n[data-theme=\"dark\"] {\n}\n")
	// Dark: re-emit github-dark scoped under the dark attribute.
	dark := render.ChromaCSS("github-dark", ".chroma")
	dark = scopeDark(dark)
	b.WriteString(dark)
	return b.String()
}

// scopeDark prefixes each rule with the dark theme attribute selector.
func scopeDark(css string) string {
	var out strings.Builder
	for _, line := range strings.Split(css, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ".chroma") {
			out.WriteString("[data-theme=\"dark\"] ")
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

var _ = time.Now // reserved for future cache-busting
