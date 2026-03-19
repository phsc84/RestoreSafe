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

// CreateStagingDir creates a temporary staging directory below tempDir.
func CreateStagingDir(tempDir, pattern string) (string, error) {
	stagingDir, err := os.MkdirTemp(tempDir, pattern)
	if err != nil {
		return "", fmt.Errorf("Failed to create local staging directory: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", err)
	}
	return stagingDir, nil
}

// CleanupStagingDir removes a staging directory and logs cleanup failures.
func CleanupStagingDir(stagingDir string, log *util.Logger) {
	if stagingDir == "" {
		return
	}
	if err := os.RemoveAll(stagingDir); err != nil && log != nil {
		log.Warn("Failed to remove staging directory %s: %v", filepath.ToSlash(stagingDir), err)
	}
}

// CleanupStagingDirDuring removes a staging directory during error recovery.
func CleanupStagingDirDuring(stagingDir, phase string, log *util.Logger) {
	if stagingDir == "" {
		return
	}
	if err := os.RemoveAll(stagingDir); err != nil && log != nil {
		log.Warn("Failed to remove staging directory %s during %s: %v", filepath.ToSlash(stagingDir), phase, err)
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
