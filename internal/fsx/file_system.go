package fsx

import (
	"RestoreSafe/internal/problem"
	"os"
	"path/filepath"
)

// ValidateSourceDirectory checks that resolved is an accessible, readable directory.
// Returns a descriptive, actionable error if any check fails.
func ValidateSourceDirectory(resolved string) error {
	info, err := os.Stat(resolved)
	if err != nil {
		return problem.Errorf("Not found or inaccessible: %w.", err).WithRemedy("Check the path in config.yaml and use forward slashes on Windows (e.g. C:/Users/Name/Documents).")
	}
	if !info.IsDir() {
		return problem.New("Path is not a directory.").WithRemedy("Provide a directory path, not a file path.")
	}
	if _, err := os.ReadDir(resolved); err != nil {
		return problem.Errorf("Directory not readable: %w.", err).WithRemedy("Check permissions and ensure this user can read the directory.")
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
		return 0, problem.New("Path is not a directory.").WithRemedy("Use only directory paths in source_directories.")
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
