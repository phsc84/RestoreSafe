package util

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

// VolumeKey returns a normalized drive/share identifier for absolute paths.
// Examples on Windows: "c:", "m:", "\\server\share".
func VolumeKey(path string) string {
	cleaned := filepath.Clean(path)
	volume := filepath.VolumeName(cleaned)
	if volume == "" {
		return ""
	}
	return strings.ToLower(volume)
}

// VolumeDisplay returns the drive/share part in a normalized display form.
func VolumeDisplay(path string) string {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume == "" {
		return ""
	}
	return filepath.ToSlash(volume)
}

// SameVolume reports whether both paths resolve to the same drive/share root.
func SameVolume(pathA, pathB string) bool {
	keyA := VolumeKey(pathA)
	keyB := VolumeKey(pathB)
	return keyA != "" && keyA == keyB
}
