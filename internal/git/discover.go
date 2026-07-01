package git

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Repo is a discovered repository.
type Repo struct {
	Slug        string // URL identity, e.g. "myrepo" or "owner/myrepo"
	Name        string // display name
	Owner       string // empty for flat namespace
	GitDir      string // absolute path to the bare repo
	Description string

	adapter *CLI

	mu         sync.Mutex
	defaultRef string
	lastCommit *Commit
	cached     bool
}

// Adapter returns the git adapter bound to this repo.
func (r *Repo) Adapter() Adapter { return r.adapter }

// SourceSpec describes one discovery root (mirrors config.RepoSource).
type SourceSpec struct {
	Path      string
	Glob      string
	Namespace string // "flat" | "owner"
}

// Discover scans the given sources and returns repos keyed by slug.
// It reads in place — nothing is copied or imported.
func Discover(specs []SourceSpec) ([]*Repo, error) {
	seen := map[string]bool{}
	var repos []*Repo
	for _, spec := range specs {
		glob := spec.Glob
		if glob == "" {
			glob = "*.git"
		}
		found, err := scanDir(spec, glob)
		if err != nil {
			return nil, err
		}
		for _, r := range found {
			if seen[r.Slug] {
				continue // first source wins on slug collision
			}
			seen[r.Slug] = true
			repos = append(repos, r)
		}
	}
	sort.Slice(repos, func(i, j int) bool {
		return strings.ToLower(repos[i].Slug) < strings.ToLower(repos[j].Slug)
	})
	return repos, nil
}

func scanDir(spec SourceSpec, glob string) ([]*Repo, error) {
	root := spec.Path
	var repos []*Repo

	if spec.Namespace == "owner" {
		// <root>/<owner>/<repo>.git
		owners, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, o := range owners {
			if !o.IsDir() {
				continue
			}
			matches, _ := filepath.Glob(filepath.Join(root, o.Name(), glob))
			for _, m := range matches {
				if r := newRepoIfBare(m, o.Name(), spec.Namespace); r != nil {
					repos = append(repos, r)
				}
			}
		}
		return repos, nil
	}

	// flat: <root>/<repo>.git
	matches, err := filepath.Glob(filepath.Join(root, glob))
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		if r := newRepoIfBare(m, "", spec.Namespace); r != nil {
			repos = append(repos, r)
		}
	}
	return repos, nil
}

// newRepoIfBare validates that dir looks like a git repo, then constructs a Repo.
func newRepoIfBare(dir, owner, namespace string) *Repo {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	gitDir := dir
	// Support a non-bare worktree by pointing at its .git directory.
	if st, e := os.Stat(filepath.Join(dir, ".git")); e == nil && st.IsDir() {
		gitDir = filepath.Join(dir, ".git")
	}
	// Heuristic: a (bare) repo has HEAD + objects.
	if _, e := os.Stat(filepath.Join(gitDir, "HEAD")); e != nil {
		return nil
	}

	base := filepath.Base(dir)
	name := strings.TrimSuffix(base, ".git")
	slug := name
	if namespace == "owner" && owner != "" {
		slug = owner + "/" + name
	}
	abs, _ := filepath.Abs(gitDir)
	r := &Repo{
		Slug:    slug,
		Name:    name,
		Owner:   owner,
		GitDir:  abs,
		adapter: NewCLI(abs),
	}
	r.Description = readDescription(gitDir)
	r.adapter.SetDescription(r.Description)
	return r
}

func readDescription(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "description"))
	if err != nil {
		return ""
	}
	d := strings.TrimSpace(string(b))
	if strings.HasPrefix(d, "Unnamed repository") {
		return ""
	}
	return d
}

// DefaultRef returns (and caches) the repo's default branch.
func (r *Repo) DefaultRef(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defaultRef != "" {
		return r.defaultRef, nil
	}
	ref, err := r.adapter.DefaultRef(ctx)
	if err != nil {
		return "", err
	}
	r.defaultRef = ref
	return ref, nil
}

// LastCommit returns the most recent commit on the default branch (cached).
func (r *Repo) LastCommit(ctx context.Context) (*Commit, error) {
	r.mu.Lock()
	if r.cached {
		c := r.lastCommit
		r.mu.Unlock()
		return c, nil
	}
	r.mu.Unlock()

	ref, err := r.DefaultRef(ctx)
	if err != nil {
		return nil, err
	}
	commits, err := r.adapter.Log(ctx, ref, "", 0, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = true
	if err != nil || len(commits) == 0 {
		return nil, err
	}
	r.lastCommit = &commits[0]
	return r.lastCommit, nil
}

// UpdatedAt is a best-effort last-activity time for sorting/listing.
func (r *Repo) UpdatedAt(ctx context.Context) time.Time {
	if c, err := r.LastCommit(ctx); err == nil && c != nil {
		return c.CommitDate
	}
	return time.Time{}
}
