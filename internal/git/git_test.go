package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeBareRepo builds a small bare repo with two commits, a branch and a tag,
// returning its git-dir.
func makeBareRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "demo.git")
	mustGit(t, "", "init", "-q", "-b", "main", work)
	env := []string{
		"GIT_AUTHOR_NAME=Tester", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=Tester", "GIT_COMMITTER_EMAIL=t@example.com",
	}
	writeFile(t, work, "README.md", "# Title\n\nhello\n")
	writeFile(t, work, "src/main.go", "package main\nfunc main() {}\n")
	mustGitEnv(t, work, env, "add", "-A")
	mustGitEnv(t, work, env, "commit", "-qm", "first")
	writeFile(t, work, "src/main.go", "package main\nfunc main() { _ = 1 }\n")
	mustGitEnv(t, work, env, "add", "-A")
	mustGitEnv(t, work, env, "commit", "-qm", "second")
	mustGitEnv(t, work, env, "tag", "v1")
	mustGitEnv(t, work, env, "branch", "dev")
	mustGit(t, "", "clone", "-q", "--bare", work, bare)
	return bare
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	mustGitEnv(t, dir, nil, args...)
}

func mustGitEnv(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, errb.String())
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterBasics(t *testing.T) {
	ctx := context.Background()
	a := NewCLI(makeBareRepo(t))

	ref, err := a.DefaultRef(ctx)
	if err != nil || ref != "main" {
		t.Fatalf("DefaultRef = %q, %v", ref, err)
	}

	refs, err := a.Refs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var branches, tags int
	for _, r := range refs {
		switch r.Kind {
		case "branch":
			branches++
		case "tag":
			tags++
		}
	}
	if branches != 2 || tags != 1 {
		t.Errorf("got %d branches, %d tags; want 2,1", branches, tags)
	}

	entries, err := a.LsTree(ctx, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	// expect README.md (blob) and src (tree); dirs sort first
	if len(entries) != 2 || !entries[0].IsDir() || entries[0].Name != "src" {
		t.Errorf("unexpected root tree: %+v", entries)
	}

	content, _, err := a.CatBlob(ctx, "main", "README.md")
	if err != nil || !bytes.Contains(content, []byte("# Title")) {
		t.Errorf("CatBlob README = %q, %v", content, err)
	}

	commits, err := a.Log(ctx, "main", "", 0, 10)
	if err != nil || len(commits) != 2 {
		t.Fatalf("Log returned %d commits, %v", len(commits), err)
	}
	if commits[0].Subject != "second" {
		t.Errorf("newest subject = %q", commits[0].Subject)
	}

	n, err := a.CommitCount(ctx, "main", "")
	if err != nil || n != 2 {
		t.Errorf("CommitCount = %d, %v", n, err)
	}
}

func TestShowDiff(t *testing.T) {
	ctx := context.Background()
	a := NewCLI(makeBareRepo(t))
	commits, _ := a.Log(ctx, "main", "", 0, 1)
	_, diffs, err := a.Show(ctx, commits[0].SHA)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range diffs {
		if d.NewPath == "src/main.go" {
			found = true
			if d.Additions == 0 && d.Deletions == 0 {
				t.Error("expected non-zero diff stats")
			}
		}
	}
	if !found {
		t.Errorf("src/main.go not in diff: %+v", diffs)
	}
}

func TestArchive(t *testing.T) {
	ctx := context.Background()
	a := NewCLI(makeBareRepo(t))
	var buf bytes.Buffer
	if err := a.Archive(ctx, "main", "zip", &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() < 4 || !bytes.HasPrefix(buf.Bytes(), []byte("PK")) {
		t.Errorf("archive is not a zip (len=%d)", buf.Len())
	}
}

func TestGrep(t *testing.T) {
	ctx := context.Background()
	a := NewCLI(makeBareRepo(t))
	hits, err := a.Grep(ctx, "main", "package", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Error("expected grep hits for 'package'")
	}
}

func TestRejectsOptionInjection(t *testing.T) {
	ctx := context.Background()
	a := NewCLI(makeBareRepo(t))
	// A rev that looks like a git option must be refused, not executed.
	evil := "--open-files-in-pager=touch /tmp/gitshelf_pwned"
	if _, err := a.Grep(ctx, evil, "x", 10); err == nil {
		t.Error("Grep accepted an option-like rev")
	}
	if _, _, err := a.CatBlob(ctx, evil, "README.md"); err == nil {
		t.Error("CatBlob accepted an option-like rev")
	}
	if _, err := a.LsTree(ctx, evil, ""); err == nil {
		t.Error("LsTree accepted an option-like rev")
	}
	if _, err := a.Log(ctx, "-n9999", "", 0, 1); err == nil {
		t.Error("Log accepted an option-like rev")
	}
	// NUL bytes are rejected too.
	if _, _, err := a.CatBlob(ctx, "main", "a\x00b"); err == nil {
		t.Error("CatBlob accepted a NUL in path")
	}
	// Legitimate refs still work.
	if _, err := a.LsTree(ctx, "main", ""); err != nil {
		t.Errorf("LsTree rejected a valid ref: %v", err)
	}
}

func TestShowMergeCommitHasDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "m.git")
	env := []string{
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com",
	}
	mustGit(t, "", "init", "-q", "-b", "main", work)
	writeFile(t, work, "base.txt", "base\n")
	mustGitEnv(t, work, env, "add", "-A")
	mustGitEnv(t, work, env, "commit", "-qm", "base")
	mustGitEnv(t, work, env, "checkout", "-q", "-b", "feature")
	writeFile(t, work, "feature.txt", "feature work\n")
	mustGitEnv(t, work, env, "add", "-A")
	mustGitEnv(t, work, env, "commit", "-qm", "feature")
	mustGitEnv(t, work, env, "checkout", "-q", "main")
	writeFile(t, work, "main.txt", "main work\n")
	mustGitEnv(t, work, env, "add", "-A")
	mustGitEnv(t, work, env, "commit", "-qm", "main change")
	mustGitEnv(t, work, env, "merge", "-q", "--no-ff", "-m", "merge feature", "feature")
	mustGit(t, "", "clone", "-q", "--bare", work, bare)

	a := NewCLI(bare)
	ctx := context.Background()
	commits, _ := a.Log(ctx, "main", "", 0, 1)
	cm, diffs, err := a.Show(ctx, commits[0].SHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Parents) != 2 {
		t.Fatalf("expected a merge commit with 2 parents, got %d", len(cm.Parents))
	}
	if len(diffs) == 0 {
		t.Error("merge commit produced an empty diff (regression)")
	}
}

func TestDiscover(t *testing.T) {
	bare := makeBareRepo(t)
	dir := filepath.Dir(bare)
	repos, err := Discover([]SourceSpec{{Path: dir, Glob: "*.git", Namespace: "flat"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Slug != "demo" {
		t.Fatalf("Discover = %+v", repos)
	}
}
