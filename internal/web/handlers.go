package web

import (
	"context"
	"html/template"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Loping151/gitshelf/internal/git"
	"github.com/Loping151/gitshelf/internal/render"
)

// dispatch is the master router for repo-scoped paths.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, s.basePath)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		s.handleList(w, r)
		return
	}
	segs := strings.Split(rel, "/")
	repo, action, rest := s.resolveRepo(segs)
	if repo == nil {
		s.notFound(w, r, "repository not found")
		return
	}
	switch action {
	case "":
		s.handleRepoHome(w, r, repo)
	case "src":
		s.handleSrc(w, r, repo, rest)
	case "raw":
		s.handleRaw(w, r, repo, rest)
	case "archive":
		s.handleArchive(w, r, repo, rest)
	case "commits":
		s.handleCommits(w, r, repo, rest)
	case "commit":
		s.handleCommit(w, r, repo, rest)
	case "compare":
		s.handleCompare(w, r, repo, rest)
	case "branches":
		s.handleRefs(w, r, repo, "branch")
	case "tags":
		s.handleRefs(w, r, repo, "tag")
	case "search":
		s.handleSearch(w, r, repo)
	case "blame":
		s.handleBlame(w, r, repo, rest)
	case "issues":
		s.handleIssues(w, r, repo, rest)
	case "pulls":
		s.handlePulls(w, r, repo, rest)
	case "releases":
		s.handleReleases(w, r, repo, rest)
	default:
		s.notFound(w, r, "unknown action: "+action)
	}
}

// ---------- view models ----------

type repoView struct {
	Slug        string
	Name        string
	Owner       string
	Description string
	DefaultRef  string
	LastCommit  *git.Commit
	Issues      int
	PullReqs    int
	Releases    int
	HasMeta     bool
}

type refView struct {
	Name   string
	Kind   string
	Target string
}

type crumb struct {
	Name string
	URL  string
}

// ---------- list & repo home ----------

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repos := s.Repos()
	views := make([]repoView, 0, len(repos))
	for _, repo := range repos {
		rv := repoView{Slug: repo.Slug, Name: repo.Name, Owner: repo.Owner, Description: repo.Description}
		rv.DefaultRef, _ = repo.DefaultRef(ctx)
		rv.LastCommit, _ = repo.LastCommit(ctx)
		c := s.provider.Counts(repo.Slug)
		rv.Issues, rv.PullReqs, rv.Releases, rv.HasMeta = c.Issues, c.PullRequests, c.Releases, c.HasData
		views = append(views, rv)
	}
	sort.SliceStable(views, func(i, j int) bool {
		li, lj := views[i].LastCommit, views[j].LastCommit
		if li != nil && lj != nil {
			return li.CommitDate.After(lj.CommitDate)
		}
		return li != nil
	})
	s.render(w, r, "list.html", s.cfg.Server.SiteName, map[string]any{"Repos": views})
}

func (s *Server) handleRepoHome(w http.ResponseWriter, r *http.Request, repo *git.Repo) {
	ctx := r.Context()
	ref, err := repo.DefaultRef(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// An empty (commit-less) repo has a HEAD but no tree; show it as empty
	// rather than 500ing.
	entries, _ := repo.Adapter().LsTree(ctx, ref, "")
	readmeHTML := s.findReadme(ctx, repo, ref, entries)
	last, _ := repo.LastCommit(ctx)
	refs, _ := s.refViews(ctx, repo)
	s.render(w, r, "repo.html", repo.Slug, map[string]any{
		"Repo":       s.repoMeta(repo),
		"Ref":        ref,
		"Dir":        "",
		"Entries":    entries,
		"ReadmeHTML": readmeHTML,
		"LastCommit": last,
		"Refs":       refs,
		"Crumbs":     []crumb{},
		"Counts":     s.provider.Counts(repo.Slug),
	})
}

// ---------- src (tree/blob) ----------

func (s *Server) handleSrc(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	rev, filePath := s.resolveRev(ctx, repo, rest)
	if rev == "" {
		s.notFound(w, r, "revision not found")
		return
	}
	if filePath == "" {
		entries, err := repo.Adapter().LsTree(ctx, rev, "")
		if err != nil {
			s.notFound(w, r, "revision not found")
			return
		}
		s.renderTree(w, r, repo, rev, "", entries)
		return
	}
	// List the path as a directory; if non-empty, it's a tree, else a blob.
	entries, err := repo.Adapter().LsTree(ctx, rev, filePath)
	if err == nil && len(entries) > 0 {
		s.renderTree(w, r, repo, rev, filePath, entries)
		return
	}
	s.renderBlob(w, r, repo, rev, filePath)
}

func (s *Server) renderTree(w http.ResponseWriter, r *http.Request, repo *git.Repo, rev, dir string, entries []git.TreeEntry) {
	ctx := r.Context()
	readmeHTML := s.findReadme(ctx, repo, rev, entries)
	refs, _ := s.refViews(ctx, repo)
	title := repo.Slug
	if dir != "" {
		title += " · " + dir
	}
	s.render(w, r, "tree.html", title, map[string]any{
		"Repo":       s.repoMeta(repo),
		"Ref":        rev,
		"Dir":        dir,
		"Entries":    entries,
		"ReadmeHTML": readmeHTML,
		"Refs":       refs,
		"Crumbs":     s.pathCrumbs(repo, rev, dir, true),
	})
}

func (s *Server) renderBlob(w http.ResponseWriter, r *http.Request, repo *git.Repo, rev, filePath string) {
	ctx := r.Context()
	content, size, err := repo.Adapter().CatBlob(ctx, rev, filePath)
	if err != nil {
		s.notFound(w, r, "file not found")
		return
	}
	fi := render.Detect(filePath, size)
	isBin := render.IsBinary(content)
	truncated := false
	maxR := s.registry.MaxBytes()
	renderContent := content
	if int64(len(content)) > maxR && !isMediaExt(fi.Ext) {
		renderContent = content[:maxR]
		truncated = true
	}
	rawURL := s.url("/"+repo.Slug, "/raw/"+rev+"/"+filePath)
	dir := path.Dir(filePath)
	if dir == "." {
		dir = ""
	}
	result, err := s.registry.Render(render.Request{
		File:      fi,
		Content:   renderContent,
		Truncated: truncated,
		RawURL:    rawURL,
		PDFEngine: s.cfg.Renderers.PDFEngine,
		IsBinary:  isBin,
		RawBase:   s.url("/"+repo.Slug, "/raw/"+rev),
		SrcBase:   s.url("/"+repo.Slug, "/src/"+rev),
		Dir:       dir,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	refs, _ := s.refViews(ctx, repo)
	s.render(w, r, "blob.html", repo.Slug+" · "+filePath, map[string]any{
		"Repo":      s.repoMeta(repo),
		"Ref":       rev,
		"Path":      filePath,
		"Result":    result,
		"Size":      size,
		"Truncated": truncated,
		"RawURL":    rawURL,
		"Refs":      refs,
		"Crumbs":    s.pathCrumbs(repo, rev, filePath, false),
	})
}

// ---------- raw ----------

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	rev, filePath := s.resolveRev(ctx, repo, rest)
	if rev == "" || filePath == "" {
		s.notFound(w, r, "file not found")
		return
	}
	size, err := repo.Adapter().BlobSize(ctx, rev, filePath)
	if err != nil {
		s.notFound(w, r, "file not found")
		return
	}
	if size > s.cfg.Renderers.MaxRawBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		s.render(w, r, "error.html", "Too large", map[string]any{
			"Code": 413, "Message": "file exceeds the configured raw size limit",
		})
		return
	}
	fi := render.Detect(filePath, size)
	// Content type from extension only (no content sniff for raw); nosniff
	// stops the browser from second-guessing us.
	w.Header().Set("Content-Type", render.MIMEFor(fi, nil))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !inlineSafe(fi.Ext) {
		// Quote-safe filename to avoid Content-Disposition header injection.
		w.Header().Set("Content-Disposition", "attachment; filename=\""+headerFilename(path.Base(filePath))+"\"")
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	// Stream from git so memory stays constant regardless of file size.
	_ = repo.Adapter().CatBlobStream(ctx, rev, filePath, w)
}

// headerFilename strips characters that could break out of a quoted
// Content-Disposition filename parameter.
func headerFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 {
			return '_'
		}
		return r
	}, name)
}

// ---------- archive ----------

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	if len(rest) == 0 {
		s.notFound(w, r, "missing revision")
		return
	}
	spec := strings.Join(rest, "/")
	format, rev := "zip", spec
	switch {
	case strings.HasSuffix(spec, ".tar.gz"):
		format, rev = "tar.gz", strings.TrimSuffix(spec, ".tar.gz")
	case strings.HasSuffix(spec, ".tgz"):
		format, rev = "tar.gz", strings.TrimSuffix(spec, ".tgz")
	case strings.HasSuffix(spec, ".zip"):
		format, rev = "zip", strings.TrimSuffix(spec, ".zip")
	}
	if !s.refExists(ctx, repo, rev) {
		s.notFound(w, r, "revision not found")
		return
	}
	filename := repo.Name + "-" + sanitizeFilename(rev)
	if format == "zip" {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+".zip\"")
	} else {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+".tar.gz\"")
	}
	_ = repo.Adapter().Archive(ctx, rev, format, w)
}

// ---------- commits / commit / compare ----------

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	rev, filePath := s.resolveRev(ctx, repo, rest)
	if rev == "" {
		rev, _ = repo.DefaultRef(ctx)
	}
	const perPage = 50
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	commits, err := repo.Adapter().Log(ctx, rev, filePath, (page-1)*perPage, perPage)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	total, _ := repo.Adapter().CommitCount(ctx, rev, filePath)
	refs, _ := s.refViews(ctx, repo)
	s.render(w, r, "commits.html", repo.Slug+" · commits", map[string]any{
		"Repo":    s.repoMeta(repo),
		"Ref":     rev,
		"Path":    filePath,
		"Commits": commits,
		"Page":    page,
		"HasNext": page*perPage < total,
		"HasPrev": page > 1,
		"Total":   total,
		"Refs":    refs,
	})
}

func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	if len(rest) == 0 {
		s.notFound(w, r, "missing sha")
		return
	}
	sha := rest[0]
	commit, diffs, err := repo.Adapter().Show(ctx, sha)
	if err != nil {
		s.notFound(w, r, "commit not found")
		return
	}
	s.render(w, r, "commit.html", repo.Slug+" · "+git.ShortSHA(sha), map[string]any{
		"Repo":   s.repoMeta(repo),
		"Commit": commit,
		"Diffs":  diffs,
		"Stats":  diffStats(diffs),
	})
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	spec := strings.Join(rest, "/")
	a, b, ok := strings.Cut(spec, "...")
	if !ok {
		a, b, ok = strings.Cut(spec, "..")
	}
	// Also accept ?base=&head= from the compare form.
	if !ok {
		if qb, qh := r.URL.Query().Get("base"), r.URL.Query().Get("head"); qb != "" && qh != "" {
			a, b, ok = qb, qh, true
		}
	}
	if !ok || a == "" || b == "" {
		s.render(w, r, "compare.html", repo.Slug+" · compare", map[string]any{
			"Repo": s.repoMeta(repo), "NeedInput": true,
		})
		return
	}
	commits, diffs, err := repo.Adapter().Compare(ctx, a, b)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "compare.html", repo.Slug+" · compare", map[string]any{
		"Repo":    s.repoMeta(repo),
		"Base":    a,
		"Head":    b,
		"Commits": commits,
		"Diffs":   diffs,
		"Stats":   diffStats(diffs),
	})
}

// ---------- refs ----------

func (s *Server) handleRefs(w http.ResponseWriter, r *http.Request, repo *git.Repo, kind string) {
	ctx := r.Context()
	all, err := repo.Adapter().Refs(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var filtered []git.Ref
	for _, ref := range all {
		if ref.Kind == kind {
			filtered = append(filtered, ref)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt) })
	s.render(w, r, "refs.html", repo.Slug+" · "+kind+"es", map[string]any{
		"Repo": s.repoMeta(repo), "Kind": kind, "Refs": filtered,
	})
}

// ---------- search ----------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, repo *git.Repo) {
	ctx := r.Context()
	q := r.URL.Query().Get("q")
	rev := r.URL.Query().Get("rev")
	if rev == "" {
		rev, _ = repo.DefaultRef(ctx)
	}
	var hits []git.GrepHit
	if q != "" {
		hits, _ = repo.Adapter().Grep(ctx, rev, q, 500)
	}
	s.render(w, r, "search.html", repo.Slug+" · search", map[string]any{
		"Repo": s.repoMeta(repo), "Query": q, "Ref": rev, "Hits": hits,
	})
}

// ---------- blame ----------

func (s *Server) handleBlame(w http.ResponseWriter, r *http.Request, repo *git.Repo, rest []string) {
	ctx := r.Context()
	rev, filePath := s.resolveRev(ctx, repo, rest)
	if rev == "" || filePath == "" {
		s.notFound(w, r, "file not found")
		return
	}
	lines, err := repo.Adapter().Blame(ctx, rev, filePath)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "blame.html", repo.Slug+" · blame · "+filePath, map[string]any{
		"Repo": s.repoMeta(repo), "Ref": rev, "Path": filePath, "Lines": lines,
		"Crumbs": s.pathCrumbs(repo, rev, filePath, false),
	})
}

// ---------- helpers ----------

func (s *Server) repoMeta(repo *git.Repo) repoView {
	return repoView{Slug: repo.Slug, Name: repo.Name, Owner: repo.Owner, Description: repo.Description}
}

func (s *Server) refViews(ctx context.Context, repo *git.Repo) ([]refView, error) {
	refs, err := repo.Adapter().Refs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]refView, 0, len(refs))
	for _, ref := range refs {
		out = append(out, refView{Name: ref.Name, Kind: ref.Kind, Target: ref.Target})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "branch"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Server) findReadme(ctx context.Context, repo *git.Repo, rev string, entries []git.TreeEntry) template.HTML {
	for _, e := range entries {
		if !e.IsDir() && isReadme(e.Name) {
			if content, _, err := repo.Adapter().CatBlob(ctx, rev, e.Path); err == nil {
				dir := path.Dir(e.Path)
				if dir == "." {
					dir = ""
				}
				return render.RenderMarkdownRel(content, render.MDOptions{
					RawBase: s.url("/"+repo.Slug, "/raw/"+rev),
					SrcBase: s.url("/"+repo.Slug, "/src/"+rev),
					Dir:     dir,
				})
			}
		}
	}
	return ""
}

// resolveRev splits segments into (rev, path) by matching the longest ref name
// prefix, else treating the first segment as the revision.
func (s *Server) resolveRev(ctx context.Context, repo *git.Repo, segs []string) (string, string) {
	if len(segs) == 0 {
		ref, _ := repo.DefaultRef(ctx)
		return ref, ""
	}
	refs, _ := repo.Adapter().Refs(ctx)
	refNames := map[string]bool{}
	for _, ref := range refs {
		refNames[ref.Name] = true
	}
	for n := len(segs); n >= 1; n-- {
		cand := strings.Join(segs[:n], "/")
		if refNames[cand] {
			return cand, strings.Join(segs[n:], "/")
		}
	}
	return segs[0], strings.Join(segs[1:], "/")
}

func (s *Server) refExists(ctx context.Context, repo *git.Repo, rev string) bool {
	refs, _ := repo.Adapter().Refs(ctx)
	for _, ref := range refs {
		if ref.Name == rev {
			return true
		}
	}
	return isHex(rev) && len(rev) >= 4
}

func (s *Server) pathCrumbs(repo *git.Repo, rev, p string, isDir bool) []crumb {
	crumbs := []crumb{{Name: repo.Name, URL: s.url("/"+repo.Slug, "/src/"+rev+"/")}}
	if p == "" {
		return crumbs
	}
	parts := strings.Split(p, "/")
	acc := ""
	for i, part := range parts {
		if acc == "" {
			acc = part
		} else {
			acc += "/" + part
		}
		isLast := i == len(parts)-1
		c := crumb{Name: part}
		if !isLast || isDir {
			c.URL = s.url("/"+repo.Slug, "/src/"+rev+"/"+acc)
		}
		crumbs = append(crumbs, c)
	}
	return crumbs
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request, msg string) {
	w.WriteHeader(http.StatusNotFound)
	s.render(w, r, "error.html", "Not found", map[string]any{"Code": 404, "Message": msg})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	s.render(w, r, "error.html", "Error", map[string]any{"Code": 500, "Message": err.Error()})
}
