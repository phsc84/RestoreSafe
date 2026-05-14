package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// ValidateSourceDirectory checks that resolved is an accessible, readable directory.
// Returns a descriptive, actionable error if any check fails.
func ValidateSourceDirectory(resolved string) error {
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("Not found or inaccessible: %w. Remedy: Check the path in config.yaml and use forward slashes on Windows (e.g. C:/Users/Name/Documents).", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("Path is not a directory. Remedy: Provide a folder path, not a file path.")
	}
	if _, err := os.ReadDir(resolved); err != nil {
		return fmt.Errorf("Directory not readable: %w. Remedy: Check permissions and ensure this user can read the folder.", err)
	}
	return nil
}

// DirectorySizeBytes returns the total size of regular files under root.
// Symlinks are skipped to avoid traversing external locations.
func DirectorySizeBytes(root string) (int64, error) {
	var total int64

	info, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("Path is not a directory. Remedy: Use only directory paths in source_folders.")
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		total += fileInfo.Size()
		return nil
	})
	if err != nil {
		return total, err
	}

	return total, nil
}
