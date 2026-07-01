package web

import (
	"net/http"
	"strings"
)

// isSecureRequest reports whether the connection is HTTPS, either directly or
// via a trusted TLS-terminating reverse proxy (X-Forwarded-Proto).
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Enabled() || !s.auth.NeedsSetup() {
		http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
		return
	}
	s.render(w, r, "setup.html", "Welcome — set up "+s.cfg.Server.SiteName, map[string]any{})
}

func (s *Server) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Enabled() || !s.auth.NeedsSetup() {
		http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, "setup.html", "Setup", map[string]any{"Error": "invalid form"})
		return
	}
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")
	if password != confirm {
		s.render(w, r, "setup.html", "Setup", map[string]any{"Error": "passwords do not match", "Username": username})
		return
	}
	if err := s.auth.CompleteSetup(username, password); err != nil {
		s.render(w, r, "setup.html", "Setup", map[string]any{"Error": err.Error(), "Username": username})
		return
	}
	// Auto-login after setup.
	token, err := s.auth.Login(username, password)
	if err == nil {
		s.auth.SetCookie(w, token, s.basePath, isSecureRequest(r))
	}
	http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Enabled() {
		http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
		return
	}
	if s.auth.NeedsSetup() {
		http.Redirect(w, r, s.url("/setup"), http.StatusSeeOther)
		return
	}
	if s.auth.Authenticated(r) {
		http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", "Sign in", map[string]any{})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, r, "login.html", "Sign in", map[string]any{"Error": "invalid form"})
		return
	}
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	token, err := s.auth.Login(username, password)
	if err != nil {
		s.render(w, r, "login.html", "Sign in", map[string]any{"Error": "Invalid username or password", "Username": username})
		return
	}
	s.auth.SetCookie(w, token, s.basePath, r.TLS != nil)
	http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := s.auth.TokenFromRequest(r); token != "" {
		s.auth.Logout(token)
	}
	s.auth.ClearCookie(w, s.basePath)
	http.Redirect(w, r, s.url("/"), http.StatusSeeOther)
}
