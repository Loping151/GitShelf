package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Loping151/gitshelf/internal/config"
)

func buildFixture(t *testing.T) (mirrors, meta string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	mirrors = filepath.Join(root, "mirrors")
	meta = filepath.Join(root, "meta")
	os.MkdirAll(mirrors, 0o755)
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com")
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		var eb bytes.Buffer
		cmd.Stderr = &eb
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, eb.String())
		}
	}
	os.MkdirAll(work, 0o755)
	run(work, "init", "-q", "-b", "main", ".")
	os.WriteFile(filepath.Join(work, "README.md"), []byte("# Demo\n\nhello world\n"), 0o644)
	os.MkdirAll(filepath.Join(work, "src"), 0o755)
	os.WriteFile(filepath.Join(work, "src/main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	run(work, "add", "-A")
	run(work, "commit", "-qm", "first")
	run(root, "clone", "-q", "--bare", work, filepath.Join(mirrors, "demo.git"))

	// metadata
	os.MkdirAll(filepath.Join(meta, "demo", "issues"), 0o755)
	os.WriteFile(filepath.Join(meta, "demo", "summary.json"), []byte(`{"repo":"demo","issues":1}`), 0o644)
	os.WriteFile(filepath.Join(meta, "demo", "issues", "1.json"),
		[]byte(`{"number":1,"title":"Hello issue","state":"OPEN","body":"**bold**"}`), 0o644)
	return mirrors, meta
}

func newTestServer(t *testing.T) *Server {
	mirrors, meta := buildFixture(t)
	cfg := config.Default()
	cfg.Cache.Dir = t.TempDir()
	cfg.Auth.Enabled = false // browsing tests exercise pages, not the auth gate
	cfg.Sources = []config.RepoSource{{Type: "directory", Path: mirrors, Glob: "*.git", Namespace: "flat"}}
	cfg.Metadata = config.Metadata{Provider: "json-export", Path: meta}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func get(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct {
		path     string
		wantCode int
		contains string
	}{
		{"/", 200, "demo"},
		{"/demo", 200, "Demo"},
		{"/demo/src/main/", 200, "README.md"},
		{"/demo/src/main/src", 200, "main.go"},
		{"/demo/src/main/src/main.go", 200, "chroma"},
		{"/demo/raw/main/README.md", 200, "# Demo"},
		{"/demo/commits/main", 200, "first"},
		{"/demo/issues", 200, "Hello issue"},
		{"/demo/issues/1", 200, "bold"},
		{"/nonexistent", 404, ""},
	}
	for _, c := range cases {
		rec := get(t, srv, c.path)
		if rec.Code != c.wantCode {
			t.Errorf("%s: code = %d, want %d", c.path, rec.Code, c.wantCode)
			continue
		}
		if c.contains != "" && !strings.Contains(rec.Body.String(), c.contains) {
			t.Errorf("%s: body missing %q", c.path, c.contains)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv := newTestServer(t)
	rec := get(t, srv, "/")
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("missing/weak CSP: %q", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff header")
	}
}

func TestArchiveDownload(t *testing.T) {
	srv := newTestServer(t)
	rec := get(t, srv, "/demo/archive/main.zip")
	if rec.Code != 200 {
		t.Fatalf("archive code = %d", rec.Code)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("PK")) {
		t.Error("archive is not a zip")
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".zip") {
		t.Errorf("missing zip disposition: %q", cd)
	}
}

func TestEmptyRepoDoesNotError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	mirrors := filepath.Join(root, "mirrors")
	os.MkdirAll(mirrors, 0o755)
	// A bare repo with no commits.
	cmd := exec.Command("git", "init", "-q", "--bare", filepath.Join(mirrors, "empty.git"))
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Cache.Dir = t.TempDir()
	cfg.Auth.Enabled = false
	cfg.Sources = []config.RepoSource{{Type: "directory", Path: mirrors, Glob: "*.git", Namespace: "flat"}}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/empty")
	if rec.Code != 200 {
		t.Errorf("empty repo home returned %d, want 200", rec.Code)
	}
}

func TestAuthGate(t *testing.T) {
	mirrors, meta := buildFixture(t)
	cfg := config.Default()
	cfg.Cache.Dir = t.TempDir()
	cfg.Auth.Enabled = true
	cfg.Sources = []config.RepoSource{{Type: "directory", Path: mirrors, Glob: "*.git", Namespace: "flat"}}
	cfg.Metadata = config.Metadata{Provider: "json-export", Path: meta}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// With auth on and no admin yet, root should redirect to /setup.
	rec := get(t, srv, "/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/setup") {
		t.Errorf("redirect to %q, want /setup", loc)
	}
}
