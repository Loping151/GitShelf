package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:templates
var templateFS embed.FS

// templateSet holds one composed template per page (base layout + partials +
// the page), keyed by page name.
type templateSet struct {
	pages map[string]*template.Template
}

// funcMap is shared by all templates. Note url-building funcs are injected per
// Server in execute() so they respect base_path.
func baseFuncMap() template.FuncMap {
	return template.FuncMap{
		"timeAgo": timeAgo,
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		},
		"shortSHA": func(s string) string {
			if len(s) > 8 {
				return s[:8]
			}
			return s
		},
		"humanSize": humanSize,
		"pathBase":  path.Base,
		"pathDir": func(p string) string {
			d := path.Dir(p)
			if d == "." || d == "/" {
				return ""
			}
			return d
		},
		"hasSuffix": strings.HasSuffix,
		"lower":     strings.ToLower,
		"add":       func(a, b int) int { return a + b },
		"sub":       func(a, b int) int { return a - b },
		"seq":       seq,
		"dict":      dict,
		// URL builders are real-implemented per-request in render(); these
		// stubs exist so templates parse at load time.
		"url":     func(parts ...string) string { return "" },
		"static":  func(p string) string { return "" },
		"repoURL": func(slug string, parts ...string) string { return "" },
		"firstLine": func(s string) string {
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				return s[:i]
			}
			return s
		},
	}
}

func loadTemplates() (*templateSet, error) {
	var partials, pages []string
	err := fs.WalkDir(templateFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := path.Base(p)
		switch {
		case name == "layout.html" || strings.HasPrefix(name, "_"):
			partials = append(partials, p)
		default:
			pages = append(pages, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	ts := &templateSet{pages: map[string]*template.Template{}}
	for _, pg := range pages {
		t := template.New("layout.html").Funcs(baseFuncMap())
		files := append(append([]string{}, partials...), pg)
		t, err := t.ParseFS(templateFS, files...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", pg, err)
		}
		ts.pages[path.Base(pg)] = t
	}
	return ts, nil
}

// pageData is the envelope passed to every template.
type pageData struct {
	Title    string
	SiteName string
	Theme    string
	BasePath string
	Authed   bool
	AuthOn   bool
	Active   string // nav highlight
	Data     any
}

// render executes a page template within the layout.
func (s *Server) render(w http.ResponseWriter, r *http.Request, page, title string, data any) {
	t, ok := s.tmpl.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	pd := pageData{
		Title:    title,
		SiteName: s.cfg.Server.SiteName,
		Theme:    s.cfg.Server.Theme,
		BasePath: s.basePath,
		Authed:   s.auth.Authenticated(r),
		AuthOn:   s.auth.Enabled(),
		Data:     data,
	}
	// Per-Server url funcs so templates can build base-path-aware links.
	clone, err := t.Clone()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clone.Funcs(template.FuncMap{
		"url":     s.url,
		"static":  func(p string) string { return s.url("/static" + p) },
		"repoURL": func(slug string, parts ...string) string { return s.url(append([]string{"/" + slug}, parts...)...) },
	})
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout.html", pd); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// --- template helper funcs ---

func timeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func seq(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

func dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict needs even number of args")
	}
	m := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		k, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		m[k] = values[i+1]
	}
	return m, nil
}
