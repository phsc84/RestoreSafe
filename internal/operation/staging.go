package operation

import (
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStagingPlan describes whether and how to use local staging to avoid same-volume contention.
type LocalStagingPlan struct {
	Enabled           bool
	SameVolume        bool
	TempDir           string
	ResolvedTempDir   string
	TempSharesVolume  bool
	ResolvedSourceDir string
	ResolvedDestDir   string
	ResolvedVolume    string
}

// PlanLocalStaging determines if local staging should be used based on source, destination, and TEMP volumes.
// It stages to local TEMP when source and dest are on the same volume AND TEMP is on a different volume.
func PlanLocalStaging(sourceDir, destDir, tempDir string) LocalStagingPlan {
	resolvedSourceDir := sourceDir
	if !filepath.IsAbs(resolvedSourceDir) {
		if absPath, err := filepath.Abs(resolvedSourceDir); err == nil {
			resolvedSourceDir = absPath
		}
	}

	resolvedDestDir := destDir
	if !filepath.IsAbs(resolvedDestDir) {
		if absPath, err := filepath.Abs(resolvedDestDir); err == nil {
			resolvedDestDir = absPath
		}
	}

	resolvedTempDir := tempDir
	if resolvedTempDir != "" && !filepath.IsAbs(resolvedTempDir) {
		if absPath, err := filepath.Abs(resolvedTempDir); err == nil {
			resolvedTempDir = absPath
		}
	}

	sameVolume := util.SameVolume(sourceDir, destDir)
	tempSharesVolume := resolvedTempDir != "" && util.SameVolume(sourceDir, resolvedTempDir)

	return LocalStagingPlan{
		Enabled:           sameVolume && resolvedTempDir != "" && !tempSharesVolume,
		SameVolume:        sameVolume,
		TempDir:           tempDir,
		ResolvedTempDir:   resolvedTempDir,
		TempSharesVolume:  tempSharesVolume,
		ResolvedSourceDir: resolvedSourceDir,
		ResolvedDestDir:   resolvedDestDir,
		ResolvedVolume:    util.VolumeDisplay(sourceDir),
	}
}

// CopyFile copies a single file from source to destination with sync for data safety.
func CopyFile(sourcePath, destinationPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("Failed to open source file %q: %w. Remedy: Check drive/network availability and read permissions.", sourcePath, err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("Failed to create local staging file %q: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", destinationPath, err)
	}
	defer destinationFile.Close()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return fmt.Errorf("Failed to copy %q to local staging: %w. Remedy: Check drive/network availability, TEMP/TMP free space, and write permissions.", sourcePath, err)
	}
	if err := destinationFile.Sync(); err != nil {
		return fmt.Errorf("Failed to sync %q to disk: %w. Remedy: Check TEMP/TMP disk space and write permissions.", destinationPath, err)
	}
	return nil
}

// StageLocalDirectory recursively copies a directory to a temporary staging location.
// Returns the path to the staged directory (under tempDir).
func StageLocalDirectory(sourceDir, destDir, tempDir string, log *util.Logger) (string, error) {
	stagingDir, err := os.MkdirTemp(tempDir, "stage-*")
	if err != nil {
		return "", fmt.Errorf("Failed to create local staging directory: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", err)
	}

	stagedPath := filepath.Join(stagingDir, filepath.Base(sourceDir))
	if err := os.MkdirAll(stagedPath, 0o750); err != nil {
		os.RemoveAll(stagingDir)
		return "", fmt.Errorf("Failed to create staging subdirectory %q: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", stagedPath, err)
	}

	// Walk source directory and copy all files.
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		os.RemoveAll(stagingDir)
		return "", fmt.Errorf("Failed to list source directory %q: %w. Remedy: Check read permissions on the backup folder.", sourceDir, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(sourceDir, entry.Name())
		destPath := filepath.Join(stagedPath, entry.Name())

		if entry.IsDir() {
			if err := filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				relPath, _ := filepath.Rel(srcPath, path)
				dPath := filepath.Join(destPath, relPath)
				if info.IsDir() {
					return os.MkdirAll(dPath, 0o750)
				}
				return CopyFile(path, dPath)
			}); err != nil {
				os.RemoveAll(stagingDir)
				return "", fmt.Errorf("Failed to copy staging directory: %w. Remedy: Check TEMP/TMP disk space and write permissions.", err)
			}
		} else {
			if err := CopyFile(srcPath, destPath); err != nil {
				os.RemoveAll(stagingDir)
				return "", fmt.Errorf("Failed to copy staging file: %w", err)
			}
		}
	}

	if log != nil {
		log.Info("Local staging enabled: backup folder staged to %s", filepath.ToSlash(stagedPath))
	}
	return stagedPath, nil
}
