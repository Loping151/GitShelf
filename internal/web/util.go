package web

import (
	"strings"

	"github.com/Loping151/gitshelf/internal/git"
)

var readmeNames = map[string]bool{
	"readme.md": true, "readme.markdown": true, "readme": true,
	"readme.txt": true, "readme.rst": true,
}

func isReadme(name string) bool {
	return readmeNames[strings.ToLower(name)]
}

var mediaExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".svg": true, ".avif": true, ".bmp": true, ".ico": true, ".pdf": true,
	".mp3": true, ".m4a": true, ".ogg": true, ".oga": true, ".wav": true,
	".flac": true, ".opus": true, ".mp4": true, ".webm": true, ".mov": true,
	".mkv": true, ".m4v": true,
}

// isMediaExt reports whether a file is served via the raw URL rather than
// inline-rendered text (so we must not truncate its bytes).
func isMediaExt(ext string) bool { return mediaExts[ext] }

// inlineSafe reports whether the raw endpoint may serve a file inline rather
// than forcing a download. We allow images/media/pdf; everything textual is
// served as text/plain (handled by MIMEFor) and is therefore safe inline too.
func inlineSafe(ext string) bool {
	switch ext {
	case ".svg":
		return false // never inline SVG as image/svg+xml from raw (XSS vector)
	}
	return true
}

func sanitizeFilename(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", "..", "-", " ", "-", ":", "-")
	return strings.Trim(r.Replace(s), "-")
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// diffStat aggregates totals for a commit/compare view.
type diffStat struct {
	Files     int
	Additions int
	Deletions int
}

func diffStats(diffs []git.FileDiff) diffStat {
	st := diffStat{Files: len(diffs)}
	for _, d := range diffs {
		st.Additions += d.Additions
		st.Deletions += d.Deletions
	}
	return st
}
