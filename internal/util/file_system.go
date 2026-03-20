package util

import (
	"fmt"
	"os"
	"path/filepath"
)

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
