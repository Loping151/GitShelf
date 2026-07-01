// Package metadata unifies externally-exported issues / PRs / releases behind a
// pluggable Provider interface. The canonical schema mirrors GitHub's GraphQL
// shape (see spec §9.3); consumers must tolerate missing fields.
package metadata

import (
	"bytes"
	"encoding/json"
	"time"
)

// connList unmarshals a field that may be EITHER a plain JSON array
// (`[ {...}, {...} ]`, the simplified spec shape) OR a GitHub GraphQL
// connection object (`{ "nodes": [ {...} ] }`, what `gh api graphql` exports).
// It behaves as a plain []T everywhere else (ranging, len, truthiness).
type connList[T any] []T

func (c *connList[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*c = nil
		return nil
	}
	if b[0] == '[' {
		return json.Unmarshal(b, (*[]T)(c))
	}
	var conn struct {
		Nodes []T `json:"nodes"`
	}
	if err := json.Unmarshal(b, &conn); err != nil {
		return err
	}
	*c = conn.Nodes
	return nil
}

// Actor is a user reference.
type Actor struct {
	Login string `json:"login"`
}

// Name returns a display name, falling back to a placeholder.
func (a *Actor) Name() string {
	if a == nil || a.Login == "" {
		return "ghost"
	}
	return a.Login
}

// Label is an issue/PR label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Milestone groups issues/PRs.
type Milestone struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// Comment is a single timeline comment.
type Comment struct {
	Author    *Actor    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

// CommentConnection wraps comments with a count.
type CommentConnection struct {
	TotalCount int       `json:"totalCount"`
	Nodes      []Comment `json:"nodes"`
}

// Issue is a canonical issue.
type Issue struct {
	Number      int               `json:"number"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	State       string            `json:"state"`       // OPEN | CLOSED
	StateReason string            `json:"stateReason"` // COMPLETED | NOT_PLANNED | REOPENED
	URL         string            `json:"url"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	Author      *Actor            `json:"author"`
	Labels      connList[Label]   `json:"labels"`
	Assignees   connList[Actor]   `json:"assignees"`
	Milestone   *Milestone        `json:"milestone"`
	Comments    CommentConnection `json:"comments"`
}

// IsPR is false for plain issues.
func (i Issue) IsPR() bool { return false }

// Commit is a PR commit node.
type PRCommit struct {
	Commit struct {
		OID             string    `json:"oid"`
		MessageHeadline string    `json:"messageHeadline"`
		AuthoredDate    time.Time `json:"authoredDate"`
		Author          struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	} `json:"commit"`
}

// PRFile is a changed file entry.
type PRFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Review is a PR review.
type Review struct {
	Author      *Actor    `json:"author"`
	Body        string    `json:"body"`
	State       string    `json:"state"` // APPROVED | CHANGES_REQUESTED | COMMENTED
	SubmittedAt time.Time `json:"submittedAt"`
}

// PullRequest is the Issue superset for PRs.
type PullRequest struct {
	Issue
	Merged       bool      `json:"merged"`
	MergedAt     time.Time `json:"mergedAt"`
	IsDraft      bool      `json:"isDraft"`
	Mergeable    string    `json:"mergeable"`
	HeadRefName  string    `json:"headRefName"`
	BaseRefName  string    `json:"baseRefName"`
	Additions    int       `json:"additions"`
	Deletions    int       `json:"deletions"`
	ChangedFiles int       `json:"changedFiles"`
	Commits      struct {
		TotalCount int        `json:"totalCount"`
		Nodes      []PRCommit `json:"nodes"`
	} `json:"commits"`
	Files struct {
		TotalCount int      `json:"totalCount"`
		Nodes      []PRFile `json:"nodes"`
	} `json:"files"`
	Reviews connList[Review] `json:"reviews"`
}

// EffectiveState returns a UI-friendly status: open|closed|merged|draft.
func (pr PullRequest) EffectiveState() string {
	switch {
	case pr.Merged:
		return "merged"
	case pr.IsDraft && pr.State == "OPEN":
		return "draft"
	case pr.State == "CLOSED":
		return "closed"
	default:
		return "open"
	}
}

// ReleaseAsset is a downloadable artifact.
type ReleaseAsset struct {
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	DownloadURL string    `json:"downloadUrl"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Release is a canonical release. Note GitHub uses "description" for the body.
type Release struct {
	TagName       string    `json:"tagName"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	IsDraft       bool      `json:"isDraft"`
	IsPrerelease  bool      `json:"isPrerelease"`
	CreatedAt     time.Time `json:"createdAt"`
	PublishedAt   time.Time `json:"publishedAt"`
	URL           string    `json:"url"`
	Author        *Actor    `json:"author"`
	ReleaseAssets struct {
		Nodes []ReleaseAsset `json:"nodes"`
	} `json:"releaseAssets"`
}

// Summary mirrors summary.json.
type Summary struct {
	Repo                string    `json:"repo"`
	ExportedAt          time.Time `json:"exportedAt"`
	Issues              int       `json:"issues"`
	PullRequests        int       `json:"pullRequests"`
	Releases            int       `json:"releases"`
	ReleaseAssetTotalMB float64   `json:"releaseAssetTotalMB"`
	SectionErrors       any       `json:"sectionErrors"`
}

// Counts is a lightweight per-repo tally for list pages.
type Counts struct {
	Issues       int
	PullRequests int
	Releases     int
	HasData      bool
}

// Provider is the pluggable metadata source.
type Provider interface {
	// Counts returns per-repo tallies for the list page (cheap).
	Counts(repo string) Counts
	Issues(repo string) ([]Issue, error)
	Issue(repo string, number int) (*Issue, error)
	PullRequests(repo string) ([]PullRequest, error)
	PullRequest(repo string, number int) (*PullRequest, error)
	Releases(repo string) ([]Release, error)
	Release(repo, tag string) (*Release, error)
}

// Nop is the empty provider used when metadata is disabled.
type Nop struct{}

func (Nop) Counts(string) Counts                          { return Counts{} }
func (Nop) Issues(string) ([]Issue, error)                { return nil, nil }
func (Nop) Issue(string, int) (*Issue, error)             { return nil, nil }
func (Nop) PullRequests(string) ([]PullRequest, error)    { return nil, nil }
func (Nop) PullRequest(string, int) (*PullRequest, error) { return nil, nil }
func (Nop) Releases(string) ([]Release, error)            { return nil, nil }
func (Nop) Release(string, string) (*Release, error)      { return nil, nil }
