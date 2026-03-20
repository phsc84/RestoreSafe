package util

import (
	"fmt"
	"io"
	"os"
)

// CopyFile copies a single file from src to dst with sync for data safety.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Failed to open source file %q: %w. Remedy: Check drive/network availability and read permissions.", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("Failed to create destination file %q: %w. Remedy: Check write permissions and free disk space.", dst, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("Failed to copy %q: %w. Remedy: Check drive/network availability, free space, and write permissions.", src, err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("Failed to sync %q to disk: %w. Remedy: Check disk space and write permissions.", dst, err)
	}
	return nil
}
