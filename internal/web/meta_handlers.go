package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Loping151/gitshelf/internal/git"
	"github.com/Loping151/gitshelf/internal/metadata"
	"github.com/Loping151/gitshelf/internal/render"
)

// ---------- issues ----------

func (s *Server) handleIssues(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	if len(rest) > 0 && rest[0] != "" {
		n, err := strconv.Atoi(rest[0])
		if err != nil {
			s.notFound(w, r, "invalid issue number")
			return
		}
		s.handleIssueDetail(w, r, repo, n)
		return
	}
	issues, _ := s.provider.Issues(repo.Slug)
	state := r.URL.Query().Get("state")
	q := strings.ToLower(r.URL.Query().Get("q"))
	var filtered []metadata.Issue
	open, closed := 0, 0
	for _, it := range issues {
		if it.State == "OPEN" {
			open++
		} else {
			closed++
		}
		if state == "open" && it.State != "OPEN" {
			continue
		}
		if state == "closed" && it.State == "OPEN" {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Title), q) {
			continue
		}
		filtered = append(filtered, it)
	}
	s.render(w, r, "issues.html", repo.Slug+" · issues", map[string]any{
		"Repo": s.repoMeta(repo), "Issues": filtered, "Kind": "issues",
		"State": state, "Query": r.URL.Query().Get("q"), "OpenCount": open, "ClosedCount": closed,
	})
}

func (s *Server) handleIssueDetail(w http.ResponseWriter, r *http.Request, repo *git.Repo, n int) {
	it, _ := s.provider.Issue(repo.Slug, n)
	if it == nil {
		s.notFound(w, r, "issue not found")
		return
	}
	s.render(w, r, "issue.html", repo.Slug+" · #"+strconv.Itoa(n), map[string]any{
		"Repo": s.repoMeta(repo), "Issue": it, "Kind": "issues",
		"BodyHTML": mdHTML(it.Body), "Comments": renderComments(it.Comments.Nodes),
	})
}

// ---------- pull requests ----------

func (s *Server) handlePulls(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	if len(rest) > 0 && rest[0] != "" {
		n, err := strconv.Atoi(rest[0])
		if err != nil {
			s.notFound(w, r, "invalid PR number")
			return
		}
		s.handlePullDetail(w, r, repo, n)
		return
	}
	prs, _ := s.provider.PullRequests(repo.Slug)
	state := r.URL.Query().Get("state")
	q := strings.ToLower(r.URL.Query().Get("q"))
	var filtered []metadata.PullRequest
	open, closed := 0, 0
	for _, pr := range prs {
		es := pr.EffectiveState()
		if es == "open" || es == "draft" {
			open++
		} else {
			closed++
		}
		if state == "open" && !(es == "open" || es == "draft") {
			continue
		}
		if state == "closed" && (es == "open" || es == "draft") {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(pr.Title), q) {
			continue
		}
		filtered = append(filtered, pr)
	}
	s.render(w, r, "pulls.html", repo.Slug+" · pulls", map[string]any{
		"Repo": s.repoMeta(repo), "Pulls": filtered, "Kind": "pulls",
		"State": state, "Query": r.URL.Query().Get("q"), "OpenCount": open, "ClosedCount": closed,
	})
}

func (s *Server) handlePullDetail(w http.ResponseWriter, r *http.Request, repo *git.Repo, n int) {
	pr, _ := s.provider.PullRequest(repo.Slug, n)
	if pr == nil {
		s.notFound(w, r, "pull request not found")
		return
	}
	s.render(w, r, "pull.html", repo.Slug+" · #"+strconv.Itoa(n), map[string]any{
		"Repo": s.repoMeta(repo), "PR": pr, "Kind": "pulls",
		"BodyHTML": mdHTML(pr.Body), "Comments": renderComments(pr.Comments.Nodes),
	})
}

// ---------- releases ----------

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	if len(rest) > 0 && rest[0] != "" {
		tag := strings.Join(rest, "/")
		s.handleReleaseDetail(w, r, repo, tag)
		return
	}
	releases, _ := s.provider.Releases(repo.Slug)
	s.render(w, r, "releases.html", repo.Slug+" · releases", map[string]any{
		"Repo": s.repoMeta(repo), "Releases": releases, "Kind": "releases",
	})
}

func (s *Server) handleReleaseDetail(w http.ResponseWriter, r *http.Request, repo *git.Repo, tag string) {
	rel, _ := s.provider.Release(repo.Slug, tag)
	if rel == nil {
		s.notFound(w, r, "release not found")
		return
	}
	s.render(w, r, "release.html", repo.Slug+" · "+rel.TagName, map[string]any{
		"Repo": s.repoMeta(repo), "Release": rel, "Kind": "releases",
		"BodyHTML": mdHTML(rel.Description),
	})
}

// ---------- helpers ----------

func mdHTML(body string) template.HTML {
	if strings.TrimSpace(body) == "" {
		return template.HTML(`<p class="muted">No description provided.</p>`)
	}
	return render.RenderMarkdown([]byte(body))
}

type commentView struct {
	Author    string
	CreatedAt time.Time
	URL       string
	Body      template.HTML
}

func renderComments(nodes []metadata.Comment) []commentView {
	out := make([]commentView, 0, len(nodes))
	for _, c := range nodes {
		out = append(out, commentView{
			Author:    c.Author.Name(),
			CreatedAt: c.CreatedAt,
			URL:       c.URL,
			Body:      mdHTML(c.Body),
		})
	}
	return out
}
