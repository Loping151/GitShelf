package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "demo")
	for _, d := range []string{"issues", "prs", "releases"} {
		os.MkdirAll(filepath.Join(repo, d), 0o755)
	}
	write(t, filepath.Join(repo, "summary.json"), `{"repo":"demo","issues":1,"pullRequests":1,"releases":1}`)
	write(t, filepath.Join(repo, "issues", "7.json"), `{"number":7,"title":"Bug","state":"OPEN","comments":{"totalCount":1,"nodes":[{"body":"hi"}]}}`)
	write(t, filepath.Join(repo, "prs", "12.json"), `{"number":12,"title":"Feature","state":"CLOSED","merged":true,"baseRefName":"main","headRefName":"f"}`)
	write(t, filepath.Join(repo, "releases", "v1.0.json"), `{"tagName":"v1.0","name":"First","description":"notes"}`)
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCounts(t *testing.T) {
	p := NewJSONExport(setup(t))
	c := p.Counts("demo")
	if !c.HasData || c.Issues != 1 || c.PullRequests != 1 || c.Releases != 1 {
		t.Errorf("counts = %+v", c)
	}
	if p.Counts("missing").HasData {
		t.Error("missing repo should have no data")
	}
}

func TestIssue(t *testing.T) {
	p := NewJSONExport(setup(t))
	it, err := p.Issue("demo", 7)
	if err != nil || it == nil {
		t.Fatalf("Issue = %v, %v", it, err)
	}
	if it.Title != "Bug" || it.State != "OPEN" {
		t.Errorf("issue = %+v", it)
	}
	if it.Comments.TotalCount != 1 {
		t.Errorf("comments = %d", it.Comments.TotalCount)
	}
	// missing issue → nil, no error
	if missing, err := p.Issue("demo", 999); err != nil || missing != nil {
		t.Errorf("missing issue = %v, %v", missing, err)
	}
}

func TestPullRequestEffectiveState(t *testing.T) {
	p := NewJSONExport(setup(t))
	pr, err := p.PullRequest("demo", 12)
	if err != nil || pr == nil {
		t.Fatal(err)
	}
	if pr.EffectiveState() != "merged" {
		t.Errorf("effective state = %q, want merged", pr.EffectiveState())
	}
}

func TestRelease(t *testing.T) {
	p := NewJSONExport(setup(t))
	rel, err := p.Release("demo", "v1.0")
	if err != nil || rel == nil {
		t.Fatal(err)
	}
	if rel.Name != "First" || rel.Description != "notes" {
		t.Errorf("release = %+v", rel)
	}
}

func TestTolerateMissingTree(t *testing.T) {
	p := NewJSONExport(t.TempDir())
	if issues, err := p.Issues("nope"); err != nil || issues != nil {
		t.Errorf("expected empty issues for missing tree, got %v, %v", issues, err)
	}
}

// The real gh-graphql export wraps labels/assignees/reviews in connection
// objects ({"nodes": [...]}); the loader must accept that, not just bare arrays.
func TestConnectionShapedFields(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "demo")
	os.MkdirAll(filepath.Join(repo, "issues"), 0o755)
	os.MkdirAll(filepath.Join(repo, "prs"), 0o755)
	write(t, filepath.Join(repo, "issues", "1.json"), `{
		"number":1,"title":"Has connection labels","state":"OPEN",
		"labels":{"nodes":[{"name":"bug"},{"name":"help wanted"}]},
		"assignees":{"nodes":[{"login":"alice"}]}}`)
	write(t, filepath.Join(repo, "prs", "2.json"), `{
		"number":2,"title":"PR","state":"OPEN",
		"reviews":{"nodes":[{"author":{"login":"bob"},"state":"APPROVED"}]}}`)

	p := NewJSONExport(root)
	issues, _ := p.Issues("demo")
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (connection-shaped labels dropped it)", len(issues))
	}
	if len(issues[0].Labels) != 2 || issues[0].Labels[0].Name != "bug" {
		t.Errorf("labels not parsed from connection: %+v", issues[0].Labels)
	}
	if len(issues[0].Assignees) != 1 {
		t.Errorf("assignees not parsed from connection: %+v", issues[0].Assignees)
	}
	prs, _ := p.PullRequests("demo")
	if len(prs) != 1 || len(prs[0].Reviews) != 1 {
		t.Fatalf("reviews not parsed from connection: %+v", prs)
	}

	// The simplified bare-array shape must still work too.
	os.MkdirAll(filepath.Join(root, "demo2", "issues"), 0o755)
	write(t, filepath.Join(root, "demo2", "issues", "1.json"),
		`{"number":1,"title":"Bare array","labels":[{"name":"x"}]}`)
	bare, _ := p.Issues("demo2")
	if len(bare) != 1 || len(bare[0].Labels) != 1 {
		t.Errorf("bare-array labels broke: %+v", bare)
	}
}

func TestActorNilSafe(t *testing.T) {
	var a *Actor
	if a.Name() != "ghost" {
		t.Errorf("nil actor name = %q", a.Name())
	}
}
