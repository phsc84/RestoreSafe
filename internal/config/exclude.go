package config

import (
	"RestoreSafe/internal/problem"
	"path"
	"strings"
)

// ExcludeMatcher decides which files and directories are left out of a
// backup (config.yaml "exclude").
//
// Pattern rules (case-insensitive, path.Match syntax: *, ?, [...]):
//   - A pattern without "/" matches a file or directory name at any depth,
//     e.g. "*.tmp" or "node_modules".
//   - A pattern with "/" is anchored at the source directory root; a leading
//     "/" is optional, e.g. "/Cache" or "Projects/*/build".
//   - A trailing "/" matches directories only, e.g. "logs/".
//   - Backslashes are treated as "/".
//
// A matching directory is skipped with its whole subtree.
type ExcludeMatcher struct {
	patterns []excludePattern
}

type excludePattern struct {
	raw      string
	glob     string // lower-case pattern without leading/trailing "/"
	anchored bool
	dirOnly  bool
}

// NewExcludeMatcher parses and validates patterns. It returns a matcher that
// matches nothing when patterns is empty.
func NewExcludeMatcher(patterns []string) (*ExcludeMatcher, error) {
	m := &ExcludeMatcher{}
	for _, raw := range patterns {
		p := strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
		if p == "" {
			return nil, problem.Errorf("Invalid exclude pattern %q: pattern is empty.", raw).WithRemedy("Remove the empty entry from 'exclude' in config.yaml.")
		}
		ep := excludePattern{raw: raw}
		if strings.HasSuffix(p, "/") {
			ep.dirOnly = true
			p = strings.TrimRight(p, "/")
		}
		if strings.HasPrefix(p, "/") {
			ep.anchored = true
			p = strings.TrimLeft(p, "/")
		}
		if strings.Contains(p, "/") {
			ep.anchored = true
		}
		if p == "" || p == "." || p == ".." || strings.Contains(p, "//") {
			return nil, problem.Errorf("Invalid exclude pattern %q.", raw).WithRemedy("Use a file or directory name (e.g. *.tmp) or a path from the source directory root (e.g. /Cache).")
		}
		for _, seg := range strings.Split(p, "/") {
			if seg == ".." || seg == "." {
				return nil, problem.Errorf("Invalid exclude pattern %q: \".\" and \"..\" are not allowed.", raw).WithRemedy("Use a path from the source directory root (e.g. /Cache).")
			}
		}
		ep.glob = strings.ToLower(p)
		if _, err := path.Match(ep.glob, ""); err != nil {
			return nil, problem.Errorf("Invalid exclude pattern %q: %v.", raw, err).WithRemedy("Check the brackets and wildcards in 'exclude' in config.yaml.")
		}
		m.patterns = append(m.patterns, ep)
	}
	return m, nil
}

// Empty reports whether the matcher has no patterns.
func (m *ExcludeMatcher) Empty() bool { return m == nil || len(m.patterns) == 0 }

// Patterns returns the patterns as configured.
func (m *ExcludeMatcher) Patterns() []string {
	if m == nil {
		return nil
	}
	out := make([]string, len(m.patterns))
	for i, p := range m.patterns {
		out[i] = p.raw
	}
	return out
}

// Match reports whether rel (relative to the source directory, "/"
// separated) is excluded.
func (m *ExcludeMatcher) Match(rel string, isDir bool) bool {
	if m.Empty() {
		return false
	}
	lower := strings.ToLower(rel)
	name := lower
	if i := strings.LastIndexByte(lower, '/'); i >= 0 {
		name = lower[i+1:]
	}
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		target := name
		if p.anchored {
			target = lower
		}
		if ok, _ := path.Match(p.glob, target); ok {
			return true
		}
	}
	return false
}
