// Package git is the Git adapter layer. It reads existing bare/mirror repos
// in place (zero-copy) by shelling out to the git CLI with argument arrays
// only — never shell string concatenation.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Adapter is the abstraction over a single repository. v1 is a CLI impl;
// a go-git/libgit2 impl can be swapped in behind this interface later.
type Adapter interface {
	DefaultRef(ctx context.Context) (string, error)
	Refs(ctx context.Context) ([]Ref, error)
	LsTree(ctx context.Context, rev, path string) ([]TreeEntry, error)
	CatBlob(ctx context.Context, rev, path string) ([]byte, int64, error)
	CatBlobStream(ctx context.Context, rev, path string, w io.Writer) error
	BlobSize(ctx context.Context, rev, path string) (int64, error)
	Log(ctx context.Context, rev, path string, skip, limit int) ([]Commit, error)
	CommitCount(ctx context.Context, rev, path string) (int, error)
	Show(ctx context.Context, sha string) (Commit, []FileDiff, error)
	Compare(ctx context.Context, a, b string) ([]Commit, []FileDiff, error)
	Blame(ctx context.Context, rev, path string) ([]BlameLine, error)
	Archive(ctx context.Context, rev, format string, w io.Writer) error
	Grep(ctx context.Context, rev, pattern string, limit int) ([]GrepHit, error)
	Description() string
}

// Ref is a branch or tag.
type Ref struct {
	Name      string
	Kind      string // "branch" | "tag"
	Target    string // commit sha
	UpdatedAt time.Time
}

// TreeEntry is one row of a tree listing.
type TreeEntry struct {
	Mode string
	Type string // "blob" | "tree" | "commit" (submodule)
	SHA  string
	Size int64  // -1 for trees
	Name string // last path component
	Path string // full path from repo root
}

// IsDir reports whether the entry is a directory (tree).
func (e TreeEntry) IsDir() bool { return e.Type == "tree" }

// Commit is a single commit's metadata.
type Commit struct {
	SHA         string
	Author      string
	AuthorEmail string
	AuthorDate  time.Time
	Committer   string
	CommitDate  time.Time
	Subject     string
	Body        string
	Parents     []string
}

// FileDiff is the diff for one file within a commit/compare.
type FileDiff struct {
	OldPath   string
	NewPath   string
	Status    string // A,M,D,R,C
	Additions int
	Deletions int
	Binary    bool
	Hunks     []Hunk
}

// Hunk is a contiguous block of diff lines.
type Hunk struct {
	Header string
	Lines  []DiffLine
}

// DiffLine is one line of a unified diff.
type DiffLine struct {
	Kind    byte // ' ', '+', '-'
	Old     int  // old line number, 0 if none
	New     int  // new line number, 0 if none
	Content string
}

// BlameLine attributes one source line to a commit.
type BlameLine struct {
	SHA     string
	Author  string
	Date    time.Time
	LineNo  int
	Content string
}

// GrepHit is a single search match.
type GrepHit struct {
	Path    string
	LineNo  int
	Content string
}

// CLI implements Adapter over the git command line.
type CLI struct {
	gitDir string
	desc   string
}

// NewCLI returns a CLI adapter for a bare repo at gitDir.
func NewCLI(gitDir string) *CLI {
	return &CLI{gitDir: gitDir}
}

func (c *CLI) Description() string { return c.desc }

// SetDescription caches the repo description (read from the description file).
func (c *CLI) SetDescription(d string) { c.desc = d }

// ErrUnsafeArg is returned when a user-controlled revision/path/pattern looks
// like a git option (leading "-") or contains a NUL byte. This prevents
// argument/option injection (e.g. a rev of "--open-files-in-pager=...").
var ErrUnsafeArg = errors.New("git: unsafe argument")

// guard rejects user-controlled values that could be interpreted as git
// options. Empty values are allowed (callers may pass an empty path).
func guard(vals ...string) error {
	for _, v := range vals {
		if v == "" {
			continue
		}
		if v[0] == '-' || strings.ContainsRune(v, 0) {
			return ErrUnsafeArg
		}
	}
	return nil
}

func (c *CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"--git-dir=" + c.gitDir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func (c *CLI) runStream(ctx context.Context, w io.Writer, args ...string) error {
	full := append([]string{"--git-dir=" + c.gitDir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var errb bytes.Buffer
	cmd.Stdout = w
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return nil
}

// DefaultRef returns the repo's default branch name (HEAD), falling back to
// main/master if HEAD is not a symbolic ref.
func (c *CLI) DefaultRef(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "symbolic-ref", "--short", "HEAD")
	if err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s, nil
		}
	}
	for _, cand := range []string{"main", "master"} {
		if _, e := c.run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+cand); e == nil {
			return cand, nil
		}
	}
	// last resort: first branch
	refs, err := c.Refs(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range refs {
		if r.Kind == "branch" {
			return r.Name, nil
		}
	}
	return "", errors.New("no branches found")
}

// Refs returns all branches and tags.
func (c *CLI) Refs(ctx context.Context) ([]Ref, error) {
	out, err := c.run(ctx, "for-each-ref",
		"--format=%(refname) %(objectname) %(creatordate:iso-strict)",
		"refs/heads", "refs/tags")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		full, target := parts[0], parts[1]
		r := Ref{Target: target}
		switch {
		case strings.HasPrefix(full, "refs/heads/"):
			r.Kind = "branch"
			r.Name = strings.TrimPrefix(full, "refs/heads/")
		case strings.HasPrefix(full, "refs/tags/"):
			r.Kind = "tag"
			r.Name = strings.TrimPrefix(full, "refs/tags/")
		default:
			continue
		}
		if len(parts) == 3 {
			if t, e := time.Parse(time.RFC3339, parts[2]); e == nil {
				r.UpdatedAt = t
			}
		}
		refs = append(refs, r)
	}
	return refs, nil
}

// LsTree lists the entries directly under path at rev (non-recursive).
func (c *CLI) LsTree(ctx context.Context, rev, path string) ([]TreeEntry, error) {
	if err := guard(rev, path); err != nil {
		return nil, err
	}
	args := []string{"ls-tree", "--long", "-z", rev}
	if path != "" {
		args = append(args, "--", path+"/")
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		// "<mode> <type> <sha> <size>\t<path>"
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(rec[:tab])
		fullPath := rec[tab+1:]
		if len(meta) < 3 {
			continue
		}
		e := TreeEntry{Mode: meta[0], Type: meta[1], SHA: meta[2], Size: -1, Path: fullPath}
		if len(meta) >= 4 && meta[3] != "-" {
			e.Size, _ = strconv.ParseInt(meta[3], 10, 64)
		}
		if i := strings.LastIndexByte(fullPath, '/'); i >= 0 {
			e.Name = fullPath[i+1:]
		} else {
			e.Name = fullPath
		}
		entries = append(entries, e)
	}
	sortTree(entries)
	return entries, nil
}

func sortTree(entries []TreeEntry) {
	// dirs first, then files; each alphabetical (case-insensitive)
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && less(entries[j], entries[j-1]); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

func less(a, b TreeEntry) bool {
	if a.IsDir() != b.IsDir() {
		return a.IsDir()
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

// CatBlob returns a file's bytes and size at rev:path.
func (c *CLI) CatBlob(ctx context.Context, rev, path string) ([]byte, int64, error) {
	if err := guard(rev, path); err != nil {
		return nil, 0, err
	}
	spec := rev + ":" + path
	out, err := c.run(ctx, "cat-file", "blob", spec)
	if err != nil {
		return nil, 0, err
	}
	return out, int64(len(out)), nil
}

// CatBlobStream streams a file's bytes to w (for raw downloads).
func (c *CLI) CatBlobStream(ctx context.Context, rev, path string, w io.Writer) error {
	if err := guard(rev, path); err != nil {
		return err
	}
	return c.runStream(ctx, w, "cat-file", "blob", rev+":"+path)
}

// BlobSize returns the size of a blob at rev:path without reading its content,
// so callers can enforce size limits before buffering.
func (c *CLI) BlobSize(ctx context.Context, rev, path string) (int64, error) {
	if err := guard(rev, path); err != nil {
		return 0, err
	}
	out, err := c.run(ctx, "cat-file", "-s", rev+":"+path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

const logFormat = "%H%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%cI%x1f%P%x1f%s%x1f%b%x1e"

func parseCommits(out []byte) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if len(f) < 9 {
			continue
		}
		cm := Commit{
			SHA: f[0], Author: f[1], AuthorEmail: f[2],
			Committer: f[4], Subject: f[7], Body: strings.TrimRight(f[8], "\n"),
		}
		cm.AuthorDate, _ = time.Parse(time.RFC3339, f[3])
		cm.CommitDate, _ = time.Parse(time.RFC3339, f[5])
		if f[6] != "" {
			cm.Parents = strings.Fields(f[6])
		}
		commits = append(commits, cm)
	}
	return commits
}

// Log returns commit history for rev (optionally limited to path), paginated.
func (c *CLI) Log(ctx context.Context, rev, path string, skip, limit int) ([]Commit, error) {
	if err := guard(rev, path); err != nil {
		return nil, err
	}
	args := []string{"log", "--format=" + logFormat}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	if skip > 0 {
		args = append(args, "--skip", strconv.Itoa(skip))
	}
	args = append(args, rev)
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// CommitCount counts reachable commits (for pagination).
func (c *CLI) CommitCount(ctx context.Context, rev, path string) (int, error) {
	if err := guard(rev, path); err != nil {
		return 0, err
	}
	args := []string{"rev-list", "--count", rev}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// Show returns a commit and its diff.
func (c *CLI) Show(ctx context.Context, sha string) (Commit, []FileDiff, error) {
	if err := guard(sha); err != nil {
		return Commit{}, nil, err
	}
	meta, err := c.run(ctx, "show", "-s", "--format="+logFormat, sha)
	if err != nil {
		return Commit{}, nil, err
	}
	commits := parseCommits(meta)
	if len(commits) == 0 {
		return Commit{}, nil, fmt.Errorf("commit %s not found", sha)
	}
	cm := commits[0]
	var diffOut []byte
	if len(cm.Parents) > 1 {
		// Merge commit: diff-tree --root yields nothing, so show the change
		// against the first parent (GitHub's default view).
		diffOut, err = c.run(ctx, "diff", "--no-color", "-r", "--find-renames",
			cm.Parents[0], sha)
	} else {
		diffOut, err = c.run(ctx, "diff-tree", "-p", "-r", "--no-color",
			"--root", "--find-renames", sha)
	}
	if err != nil {
		return cm, nil, err
	}
	return cm, parseUnifiedDiff(diffOut), nil
}

// Compare returns the commits and diff between a and b (a..b).
func (c *CLI) Compare(ctx context.Context, a, b string) ([]Commit, []FileDiff, error) {
	if err := guard(a, b); err != nil {
		return nil, nil, err
	}
	log, err := c.run(ctx, "log", "--format="+logFormat, a+".."+b)
	if err != nil {
		return nil, nil, err
	}
	diffOut, err := c.run(ctx, "diff", "--no-color", "-r", "--find-renames", a+"..."+b)
	if err != nil {
		return parseCommits(log), nil, err
	}
	return parseCommits(log), parseUnifiedDiff(diffOut), nil
}

// Blame attributes each line of a file at rev.
func (c *CLI) Blame(ctx context.Context, rev, path string) ([]BlameLine, error) {
	if err := guard(rev, path); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, "blame", "-w", "--line-porcelain", rev, "--", path)
	if err != nil {
		return nil, err
	}
	return parseBlame(out), nil
}

// Archive streams a zip/tar.gz of rev to w.
func (c *CLI) Archive(ctx context.Context, rev, format string, w io.Writer) error {
	if err := guard(rev); err != nil {
		return err
	}
	gf := "zip"
	if format == "tar.gz" || format == "tgz" {
		gf = "tar.gz"
	}
	return c.runStream(ctx, w, "archive", "--format="+gf, rev)
}

// Grep searches tracked content at rev. Limit caps the number of hits.
func (c *CLI) Grep(ctx context.Context, rev, pattern string, limit int) ([]GrepHit, error) {
	if err := guard(rev); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, "grep", "-n", "-I", "--no-color",
		"--fixed-strings", "--ignore-case", "-e", pattern, rev)
	if err != nil {
		// git grep exits non-zero when there are no matches; treat as empty.
		return nil, nil
	}
	var hits []GrepHit
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		// format: <rev>:<path>:<lineno>:<content>
		rest := strings.TrimPrefix(line, rev+":")
		p1 := strings.IndexByte(rest, ':')
		if p1 < 0 {
			continue
		}
		path := rest[:p1]
		rest = rest[p1+1:]
		p2 := strings.IndexByte(rest, ':')
		if p2 < 0 {
			continue
		}
		ln, _ := strconv.Atoi(rest[:p2])
		hits = append(hits, GrepHit{Path: path, LineNo: ln, Content: rest[p2+1:]})
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

// ShortSHA truncates a SHA to 8 chars for display.
func ShortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
