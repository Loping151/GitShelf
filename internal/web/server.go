// Package web is the HTTP layer: routing, templates, static assets and auth
// gating. It binds the git adapter, render pipeline and metadata provider into
// a GitHub-style read-only browsing UI.
package web

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/Loping151/gitshelf/internal/auth"
	"github.com/Loping151/gitshelf/internal/config"
	"github.com/Loping151/gitshelf/internal/git"
	"github.com/Loping151/gitshelf/internal/metadata"
	"github.com/Loping151/gitshelf/internal/render"
)

// Server holds all wired dependencies.
type Server struct {
	cfg      config.Config
	basePath string

	mu         sync.RWMutex
	repos      []*git.Repo
	repoBySlug map[string]*git.Repo

	registry *render.Registry
	provider metadata.Provider
	auth     *auth.Manager
	tmpl     *templateSet
	mux      http.Handler
}

// New builds a Server from config, discovering repositories up front.
func New(cfg config.Config) (*Server, error) {
	specs := make([]git.SourceSpec, len(cfg.Sources))
	for i, s := range cfg.Sources {
		specs[i] = git.SourceSpec{Path: s.Path, Glob: s.Glob, Namespace: s.Namespace}
	}
	repos, err := git.Discover(specs)
	if err != nil {
		return nil, err
	}

	var provider metadata.Provider = metadata.Nop{}
	if cfg.Metadata.Provider == "json-export" {
		provider = metadata.NewJSONExport(cfg.Metadata.Path)
	}

	am, err := auth.New(cfg.Auth.Enabled, cfg.Cache.Dir)
	if err != nil {
		return nil, err
	}

	ts, err := loadTemplates()
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:        cfg,
		basePath:   cfg.Server.BasePath,
		repos:      repos,
		repoBySlug: map[string]*git.Repo{},
		registry:   render.NewRegistry(cfg.Renderers.Disable, cfg.Renderers.MaxRenderBytes),
		provider:   provider,
		auth:       am,
		tmpl:       ts,
	}
	for _, r := range repos {
		s.repoBySlug[r.Slug] = r
	}
	s.mux = s.routes()
	return s, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// Repos returns the discovered repositories.
func (s *Server) Repos() []*git.Repo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repos
}

// routes builds the middleware chain and the master dispatcher.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets and dynamically-generated CSS live under /static.
	s.registerStatic(mux)

	// Auth endpoints.
	mux.HandleFunc("GET "+s.path("/setup"), s.handleSetupForm)
	mux.HandleFunc("POST "+s.path("/setup"), s.handleSetupSubmit)
	mux.HandleFunc("GET "+s.path("/login"), s.handleLoginForm)
	mux.HandleFunc("POST "+s.path("/login"), s.handleLoginSubmit)
	mux.HandleFunc("POST "+s.path("/logout"), s.handleLogout)

	// Everything else routes through the master dispatcher.
	mux.HandleFunc(s.path("/"), s.dispatch)

	return s.withMiddleware(mux)
}

// path joins the configured base path with p.
func (s *Server) path(p string) string {
	if s.basePath == "" {
		return p
	}
	if p == "/" {
		return s.basePath + "/"
	}
	return s.basePath + p
}

// url builds an absolute (base-path-aware) URL for templates.
func (s *Server) url(parts ...string) string {
	var b strings.Builder
	b.WriteString(s.basePath)
	for _, p := range parts {
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			b.WriteByte('/')
		}
		b.WriteString(p)
	}
	out := b.String()
	if out == "" {
		return "/"
	}
	return out
}

// withMiddleware applies security headers and the auth gate.
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setSecurityHeaders(w)
		// Auth gate: allow static + auth pages, otherwise require a session.
		if s.auth.Enabled() {
			p := r.URL.Path
			switch {
			case strings.HasPrefix(p, s.path("/static/")):
			case p == s.path("/setup") && s.auth.NeedsSetup():
			case p == s.path("/login"):
			case p == s.path("/logout"):
			case s.auth.NeedsSetup():
				http.Redirect(w, r, s.url("/setup"), http.StatusSeeOther)
				return
			case !s.auth.Authenticated(r):
				http.Redirect(w, r, s.url("/login"), http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "no-referrer")
	// CSP: self only; media/img from self; styles inline allowed for themed
	// highlight blocks already pre-sanitized; no remote scripts.
	h.Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data: https:; media-src 'self'; "+
			"object-src 'self'; frame-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"script-src 'self'; base-uri 'none'; form-action 'self'")
}

// resolveRepo finds the repo whose slug is the longest matching prefix of segs.
// Returns the repo, the action segment (or ""), and the remaining segments.
func (s *Server) resolveRepo(segs []string) (*git.Repo, string, []string) {
	maxDepth := 2
	if len(segs) < maxDepth {
		maxDepth = len(segs)
	}
	for n := maxDepth; n >= 1; n-- {
		slug := strings.Join(segs[:n], "/")
		if repo, ok := s.repoBySlug[slug]; ok {
			action := ""
			rest := []string{}
			if len(segs) > n {
				action = segs[n]
				rest = segs[n+1:]
			}
			return repo, action, rest
		}
	}
	return nil, "", nil
}

// reqContext returns a per-request context.
func reqContext(r *http.Request) context.Context { return r.Context() }
