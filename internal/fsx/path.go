package fsx

import (
	"path/filepath"
	"strings"
)

// ResolveDir returns path unchanged if it is absolute, otherwise joins it with base.
func ResolveDir(path, base string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

// SourceDuplicateWarningFmt is the warning shown when a source directory
// resolves to the same normalized path as an earlier entry. The single %s is
// the resolved path of the first (kept) occurrence.
const SourceDuplicateWarningFmt = "identical duplicate of %s; this entry will be skipped"

// NormalizePathKey returns a canonical lowercase key for path deduplication.
// Cleans the path, normalises separators to backslash, and lowercases the result.
func NormalizePathKey(path string) string {
	cleaned := filepath.Clean(path)
	cleaned = strings.ReplaceAll(cleaned, "/", `\`)
	return strings.ToLower(cleaned)
}
