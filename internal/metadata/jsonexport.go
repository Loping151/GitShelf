package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// JSONExport reads metadata from a directory tree:
//
//	<root>/<repo>/summary.json
//	<root>/<repo>/issues/<number>.json
//	<root>/<repo>/prs/<number>.json
//	<root>/<repo>/releases/<tag>.json
//
// It tolerates missing files/fields — a repo with no metadata simply yields
// empty slices.
type JSONExport struct {
	root string
}

// NewJSONExport returns a provider rooted at dir.
func NewJSONExport(dir string) *JSONExport {
	return &JSONExport{root: dir}
}

func (j *JSONExport) repoDir(repo string) string {
	// repo is a slug like "owner/name" or "name"; keep it as a relative path
	// but guard against traversal.
	clean := filepath.Clean("/" + repo)
	return filepath.Join(j.root, clean)
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// Counts reads summary.json (or counts files) for the list page.
func (j *JSONExport) Counts(repo string) Counts {
	var s Summary
	if readJSON(filepath.Join(j.repoDir(repo), "summary.json"), &s) {
		return Counts{Issues: s.Issues, PullRequests: s.PullRequests, Releases: s.Releases, HasData: true}
	}
	c := Counts{
		Issues:       countFiles(filepath.Join(j.repoDir(repo), "issues")),
		PullRequests: countFiles(filepath.Join(j.repoDir(repo), "prs")),
		Releases:     countFiles(filepath.Join(j.repoDir(repo), "releases")),
	}
	c.HasData = c.Issues+c.PullRequests+c.Releases > 0
	return c
}

func countFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// Issues lists all issues for repo, newest first.
func (j *JSONExport) Issues(repo string) ([]Issue, error) {
	dir := filepath.Join(j.repoDir(repo), "issues")
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, nil // tolerate absence
	}
	var out []Issue
	for _, f := range files {
		var it Issue
		if readJSON(f, &it) {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number > out[b].Number })
	return out, nil
}

// Issue loads a single issue by number.
func (j *JSONExport) Issue(repo string, number int) (*Issue, error) {
	var it Issue
	if readJSON(filepath.Join(j.repoDir(repo), "issues", itoa(number)+".json"), &it) {
		return &it, nil
	}
	return nil, nil
}

// PullRequests lists all PRs for repo, newest first.
func (j *JSONExport) PullRequests(repo string) ([]PullRequest, error) {
	dir := filepath.Join(j.repoDir(repo), "prs")
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, nil
	}
	var out []PullRequest
	for _, f := range files {
		var pr PullRequest
		if readJSON(f, &pr) {
			out = append(out, pr)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number > out[b].Number })
	return out, nil
}

// PullRequest loads a single PR by number.
func (j *JSONExport) PullRequest(repo string, number int) (*PullRequest, error) {
	var pr PullRequest
	if readJSON(filepath.Join(j.repoDir(repo), "prs", itoa(number)+".json"), &pr) {
		return &pr, nil
	}
	return nil, nil
}

// Releases lists all releases for repo, newest first.
func (j *JSONExport) Releases(repo string) ([]Release, error) {
	dir := filepath.Join(j.repoDir(repo), "releases")
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, nil
	}
	var out []Release
	for _, f := range files {
		var rel Release
		if readJSON(f, &rel) {
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].PublishedAt.After(out[b].PublishedAt) })
	return out, nil
}

// Release loads a release by tag.
func (j *JSONExport) Release(repo, tag string) (*Release, error) {
	// tag may contain slashes or dots; sanitize to a base filename
	safe := filepath.Base(filepath.Clean("/" + tag))
	var rel Release
	if readJSON(filepath.Join(j.repoDir(repo), "releases", safe+".json"), &rel) {
		return &rel, nil
	}
	// fall back to scanning (tag may not match filename exactly)
	all, _ := j.Releases(repo)
	for i := range all {
		if all[i].TagName == tag {
			return &all[i], nil
		}
	}
	return nil, nil
}

func jsonFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

func itoa(n int) string {
	// small helper to avoid importing strconv just for this
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
