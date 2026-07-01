package git

import (
	"strconv"
	"strings"
	"time"
)

// parseUnifiedDiff parses `git diff`/`diff-tree -p` output into FileDiffs.
func parseUnifiedDiff(out []byte) []FileDiff {
	var files []FileDiff
	var cur *FileDiff
	var hunk *Hunk
	var oldLn, newLn int

	flushFile := func() {
		if cur != nil {
			if hunk != nil {
				cur.Hunks = append(cur.Hunks, *hunk)
				hunk = nil
			}
			files = append(files, *cur)
			cur = nil
		}
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			cur = &FileDiff{Status: "M"}
			// "diff --git a/old b/new"
			if p := strings.TrimPrefix(line, "diff --git "); p != "" {
				if a, b, ok := splitDiffPaths(p); ok {
					cur.OldPath, cur.NewPath = a, b
				}
			}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "new file"):
			cur.Status = "A"
		case strings.HasPrefix(line, "deleted file"):
			cur.Status = "D"
		case strings.HasPrefix(line, "rename from "):
			cur.Status = "R"
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			cur.NewPath = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "Binary files"):
			cur.Binary = true
		// The ---/+++ file headers only appear in the pre-hunk preamble. Once
		// inside a hunk, a line like "--- foo" is a *removed content* line whose
		// text starts with "-- " (e.g. a SQL comment), not a header.
		case hunk == nil && strings.HasPrefix(line, "--- "):
			if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" {
				cur.OldPath = stripDiffPrefix(p)
			}
		case hunk == nil && strings.HasPrefix(line, "+++ "):
			if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
				cur.NewPath = stripDiffPrefix(p)
			}
		case strings.HasPrefix(line, "@@"):
			if hunk != nil {
				cur.Hunks = append(cur.Hunks, *hunk)
			}
			hunk = &Hunk{Header: line}
			oldLn, newLn = parseHunkHeader(line)
		case hunk != nil && len(line) > 0:
			switch line[0] {
			case '+':
				hunk.Lines = append(hunk.Lines, DiffLine{Kind: '+', New: newLn, Content: line[1:]})
				newLn++
				cur.Additions++
			case '-':
				hunk.Lines = append(hunk.Lines, DiffLine{Kind: '-', Old: oldLn, Content: line[1:]})
				oldLn++
				cur.Deletions++
			case ' ':
				hunk.Lines = append(hunk.Lines, DiffLine{Kind: ' ', Old: oldLn, New: newLn, Content: line[1:]})
				oldLn++
				newLn++
			case '\\': // "\ No newline at end of file"
			}
		}
	}
	flushFile()
	return files
}

func splitDiffPaths(p string) (string, string, bool) {
	// Handles "a/path b/path"; paths without spaces (common case).
	if i := strings.Index(p, " b/"); i >= 0 {
		return stripDiffPrefix(p[:i]), stripDiffPrefix(p[i+1:]), true
	}
	return "", "", false
}

func stripDiffPrefix(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

func parseHunkHeader(h string) (oldStart, newStart int) {
	// @@ -oldStart,oldCount +newStart,newCount @@ [optional section heading]
	// Only the two range tokens between the first and second "@@" are line
	// numbers; the trailing section heading may itself contain -/+ tokens.
	parts := strings.Split(h, " ")
	at := 0
	for _, p := range parts {
		if p == "@@" {
			at++
			if at == 2 {
				break
			}
			continue
		}
		if at != 1 {
			continue
		}
		if strings.HasPrefix(p, "-") {
			oldStart = atoiFirst(p[1:])
		} else if strings.HasPrefix(p, "+") {
			newStart = atoiFirst(p[1:])
		}
	}
	return
}

func atoiFirst(s string) int {
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// parseBlame parses `git blame --line-porcelain` output.
func parseBlame(out []byte) []BlameLine {
	var result []BlameLine
	lines := strings.Split(string(out), "\n")
	commitInfo := map[string]struct {
		author string
		when   time.Time
	}{}
	var curSHA, curAuthor string
	var curTime time.Time
	var curLineNo int
	var authorTime int64

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if line[0] == '\t' {
			info := commitInfo[curSHA]
			result = append(result, BlameLine{
				SHA: curSHA, Author: info.author, Date: info.when,
				LineNo: curLineNo, Content: line[1:],
			})
			continue
		}
		fields := strings.Fields(line)
		switch {
		case len(fields[0]) == 40 && len(fields) >= 3:
			curSHA = fields[0]
			curLineNo, _ = strconv.Atoi(fields[2])
		case fields[0] == "author" && len(fields) >= 2:
			curAuthor = strings.TrimPrefix(line, "author ")
		case fields[0] == "author-time" && len(fields) >= 2:
			authorTime, _ = strconv.ParseInt(fields[1], 10, 64)
			curTime = time.Unix(authorTime, 0)
			commitInfo[curSHA] = struct {
				author string
				when   time.Time
			}{curAuthor, curTime}
		}
	}
	return result
}
