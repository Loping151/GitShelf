package git

import "testing"

// A removed line whose text begins with "-- " (e.g. a SQL comment) must be
// parsed as a deletion, not mistaken for the "--- " file header.
func TestDiffContentLooksLikeHeader(t *testing.T) {
	diff := "diff --git a/q.sql b/q.sql\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/q.sql\n" +
		"+++ b/q.sql\n" +
		"@@ -1,3 +1,3 @@\n" +
		" select 1;\n" +
		"--- get all users\n" +
		"++ added comment\n" +
		" select 2;\n"
	files := parseUnifiedDiff([]byte(diff))
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.OldPath != "q.sql" || f.NewPath != "q.sql" {
		t.Errorf("paths corrupted: old=%q new=%q", f.OldPath, f.NewPath)
	}
	if f.Deletions != 1 || f.Additions != 1 {
		t.Errorf("stats wrong: +%d -%d, want +1 -1", f.Additions, f.Deletions)
	}
	var sawDel, sawAdd bool
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Kind == '-' && l.Content == "-- get all users" {
				sawDel = true
			}
			if l.Kind == '+' && l.Content == "+ added comment" {
				sawAdd = true
			}
		}
	}
	if !sawDel || !sawAdd {
		t.Errorf("content lines lost: sawDel=%v sawAdd=%v", sawDel, sawAdd)
	}
}

// The hunk header's trailing section heading may contain -/+ tokens; only the
// two range fields are line numbers.
func TestParseHunkHeaderWithHeading(t *testing.T) {
	cases := []struct {
		h            string
		wantO, wantN int
	}{
		{"@@ -5,7 +10,6 @@ func sub() { return a - b }", 5, 10},
		{"@@ -1 +1 @@", 1, 1},
		{"@@ -12,3 +20,4 @@ x -> y +z", 12, 20},
	}
	for _, c := range cases {
		o, n := parseHunkHeader(c.h)
		if o != c.wantO || n != c.wantN {
			t.Errorf("%q -> (%d,%d), want (%d,%d)", c.h, o, n, c.wantO, c.wantN)
		}
	}
}
