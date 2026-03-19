package testutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// StageLocalDirectory recursively copies sourceDir to a temporary directory under tempDir.
// It returns the staged root path that mirrors sourceDir contents.
func StageLocalDirectory(sourceDir, tempDir string) (string, error) {
	stagingDir, err := os.MkdirTemp(tempDir, "stage-*")
	if err != nil {
		return "", fmt.Errorf("failed to create local staging directory: %w", err)
	}

	stagedPath := filepath.Join(stagingDir, filepath.Base(sourceDir))
	if err := os.MkdirAll(stagedPath, 0o750); err != nil {
		_ = os.RemoveAll(stagingDir)
		return "", fmt.Errorf("failed to create staging subdirectory %q: %w", stagedPath, err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return "", fmt.Errorf("failed to list source directory %q: %w", sourceDir, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(sourceDir, entry.Name())
		destPath := filepath.Join(stagedPath, entry.Name())

		if entry.IsDir() {
			err := filepath.Walk(srcPath, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				relPath, relErr := filepath.Rel(srcPath, path)
				if relErr != nil {
					return relErr
				}
				targetPath := filepath.Join(destPath, relPath)
				if info.IsDir() {
					return os.MkdirAll(targetPath, 0o750)
				}
				return copyFile(path, targetPath)
			})
			if err != nil {
				_ = os.RemoveAll(stagingDir)
				return "", fmt.Errorf("failed to copy staging directory: %w", err)
			}
			continue
		}

		if err := copyFile(srcPath, destPath); err != nil {
			_ = os.RemoveAll(stagingDir)
			return "", fmt.Errorf("failed to copy staging file: %w", err)
		}
	}

	return stagedPath, nil
}

func copyFile(sourcePath, destinationPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}

	return destinationFile.Sync()
}
