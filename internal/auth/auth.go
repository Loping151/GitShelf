// Package auth provides optional access control with a first-run setup wizard.
//
// When auth is enabled and no admin credential has been configured yet, every
// request is redirected to a one-time /setup page where the operator chooses a
// username and password. The credential (bcrypt hash) is persisted under the
// cache dir; sessions are signed, http-only cookies kept in memory.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName     = "gitshelf_session"
	sessionTTL     = 30 * 24 * time.Hour
	credFileName   = "admin.json"
	minPasswordLen = 8
)

// credential is the persisted admin record.
type credential struct {
	Username  string    `json:"username"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"createdAt"`
}

// Manager holds auth state.
type Manager struct {
	enabled  bool
	credPath string

	mu       sync.RWMutex
	cred     *credential
	sessions map[string]session
}

type session struct {
	username string
	expires  time.Time
}

// New constructs a Manager. When enabled, it loads any existing credential
// from dataDir.
func New(enabled bool, dataDir string) (*Manager, error) {
	m := &Manager{
		enabled:  enabled,
		credPath: filepath.Join(dataDir, credFileName),
		sessions: map[string]session{},
	}
	if !enabled {
		return m, nil
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(m.credPath); err == nil {
		var c credential
		if json.Unmarshal(b, &c) == nil && c.Hash != "" {
			m.cred = &c
		}
	}
	return m, nil
}

// Enabled reports whether auth gating is active.
func (m *Manager) Enabled() bool { return m.enabled }

// NeedsSetup is true when auth is on but no admin credential exists yet.
func (m *Manager) NeedsSetup() bool {
	if !m.enabled {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cred == nil
}

// CompleteSetup creates the admin credential (only valid during first run).
func (m *Manager) CompleteSetup(username, password string) error {
	if !m.NeedsSetup() {
		return errors.New("setup already completed")
	}
	if len(username) < 1 {
		return errors.New("username is required")
	}
	if len(password) < minPasswordLen {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	c := &credential{Username: username, Hash: string(hash), CreatedAt: time.Now()}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(m.credPath, b, 0o600); err != nil {
		return err
	}
	m.mu.Lock()
	m.cred = c
	m.mu.Unlock()
	return nil
}

// Login verifies credentials and returns a new session token on success.
func (m *Manager) Login(username, password string) (string, error) {
	m.mu.RLock()
	c := m.cred
	m.mu.RUnlock()
	if c == nil {
		return "", errors.New("not configured")
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(c.Username)) == 1
	passErr := bcrypt.CompareHashAndPassword([]byte(c.Hash), []byte(password))
	if !userOK || passErr != nil {
		return "", errors.New("invalid username or password")
	}
	token := newToken()
	now := time.Now()
	m.mu.Lock()
	// Opportunistically prune expired sessions so the map can't grow forever.
	for t, s := range m.sessions {
		if now.After(s.expires) {
			delete(m.sessions, t)
		}
	}
	m.sessions[token] = session{username: username, expires: now.Add(sessionTTL)}
	m.mu.Unlock()
	return token, nil
}

// Logout invalidates a session token.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// userFor returns the session user for a token, if valid.
func (m *Manager) userFor(token string) (string, bool) {
	m.mu.RLock()
	s, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok || time.Now().After(s.expires) {
		return "", false
	}
	return s.username, true
}

// Authenticated reports whether the request carries a valid session.
func (m *Manager) Authenticated(r *http.Request) bool {
	if !m.enabled {
		return true
	}
	ck, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	_, ok := m.userFor(ck.Value)
	return ok
}

// SetCookie writes the session cookie on login.
func (m *Manager) SetCookie(w http.ResponseWriter, token, basePath string, secure bool) {
	path := basePath
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

// ClearCookie removes the session cookie on logout.
func (m *Manager) ClearCookie(w http.ResponseWriter, basePath string) {
	path := basePath
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: path,
		HttpOnly: true, MaxAge: -1,
	})
}

// TokenFromRequest returns the raw session token (for logout).
func (m *Manager) TokenFromRequest(r *http.Request) string {
	if ck, err := r.Cookie(cookieName); err == nil {
		return ck.Value
	}
	return ""
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
