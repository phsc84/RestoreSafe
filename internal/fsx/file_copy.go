package fsx

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// CopyFile copies a single file from src to dst with sync for data safety. It
// stops when ctx is cancelled and adds the bytes copied to done (may be nil).
func CopyFile(ctx context.Context, src, dst string, done *atomic.Int64) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Failed to open source file %q: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("Failed to create destination file %q: %w", dst, err)
	}
	defer dstFile.Close()

	w := &ContextWriter{Ctx: ctx, W: &CountingWriter{W: dstFile, Total: done}}
	if _, err := io.Copy(w, srcFile); err != nil {
		return fmt.Errorf("Failed to copy %q: %w", src, err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("Failed to sync %q to disk: %w", dst, err)
	}
	return nil
}
