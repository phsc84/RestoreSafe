package util

import (
	"strings"
	"testing"
)

func TestExcludeMatcher(t *testing.T) {
	t.Parallel()

	m, err := NewExcludeMatcher([]string{"*.tmp", "node_modules", "~$*", "/Cache", `Projects\*\build`, "logs/"})
	if err != nil {
		t.Fatalf("NewExcludeMatcher: %v", err)
	}
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"a.tmp", false, true},
		{"deep/nested/B.TMP", false, true},
		{"a.tmpx", false, false},
		{"web/node_modules", true, true},
		{"web/node_modules_old", true, false},
		{"docs/~$report.docx", false, true},
		{"Cache", true, true},
		{"cache", true, true},
		{"sub/Cache", true, false},
		{"Projects/app/build", true, true},
		{"Projects/app/src/build", true, false},
		{"logs", true, true},
		{"logs", false, false},
		{"app/logs", true, true},
		{"report.docx", false, false},
	}
	for _, tc := range cases {
		if got := m.Match(tc.rel, tc.isDir); got != tc.want {
			t.Fatalf("Match(%q, dir=%v) = %v, want %v", tc.rel, tc.isDir, got, tc.want)
		}
	}
	if len(m.Patterns()) != 6 {
		t.Fatalf("Patterns() = %v", m.Patterns())
	}
}

func TestExcludeMatcherEmpty(t *testing.T) {
	t.Parallel()

	m, err := NewExcludeMatcher(nil)
	if err != nil || !m.Empty() || m.Match("anything", false) {
		t.Fatalf("empty matcher misbehaves: %v", err)
	}
	var nilMatcher *ExcludeMatcher
	if nilMatcher.Match("x", false) || !nilMatcher.Empty() {
		t.Fatal("nil matcher must match nothing")
	}
}

func TestExcludeMatcherRejectsInvalidPatterns(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"", "  ", "/", "[abc", "../x", "a/../b", "a//b", "."} {
		if _, err := NewExcludeMatcher([]string{p}); err == nil || !strings.Contains(err.Error(), "Invalid exclude pattern") {
			t.Fatalf("pattern %q: expected error, got %v", p, err)
		}
	}
}
