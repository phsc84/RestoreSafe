package util

import "path/filepath"

// ResolveDir returns path unchanged if it is absolute, otherwise joins it with base.
func ResolveDir(path, base string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}
