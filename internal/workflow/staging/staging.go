// Package staging plans and manages local staging directories in TEMP, used
// when source and target share a drive.
package staging

import (
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/logging"
	"fmt"
	"os"
	"path/filepath"
)

// Plan describes whether and how to use local staging to avoid same-volume contention.
type Plan struct {
	Enabled         bool
	SameVolume      bool
	ResolvedTempDir string
}

// PlanLocal determines if local staging should be used based on source, destination, and TEMP volumes.
// It stages to local TEMP when source and dest are on the same volume AND TEMP is on a different volume.
func PlanLocal(sourceDir, destDir, tempDir string) Plan {
	resolvedTempDir := tempDir
	if resolvedTempDir != "" && !filepath.IsAbs(resolvedTempDir) {
		if absPath, err := filepath.Abs(resolvedTempDir); err == nil {
			resolvedTempDir = absPath
		}
	}

	sameVolume := fsx.SameVolume(sourceDir, destDir)
	tempSharesVolume := resolvedTempDir != "" && fsx.SameVolume(sourceDir, resolvedTempDir)

	return Plan{
		Enabled:         sameVolume && resolvedTempDir != "" && !tempSharesVolume,
		SameVolume:      sameVolume,
		ResolvedTempDir: resolvedTempDir,
	}
}

// CreateDir creates a temporary staging directory below tempDir.
func CreateDir(tempDir, pattern string) (string, error) {
	stagingDir, err := os.MkdirTemp(tempDir, pattern)
	if err != nil {
		return "", fmt.Errorf("Failed to create local staging directory: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", err)
	}
	return stagingDir, nil
}

// CleanupDir removes a staging directory and logs cleanup failures.
func CleanupDir(stagingDir string, log *logging.Logger) {
	if stagingDir == "" {
		return
	}
	if err := os.RemoveAll(stagingDir); err != nil {
		if log != nil {
			log.Warn("Failed to remove staging directory %s: %v", filepath.ToSlash(stagingDir), err)
		}
	} else if log != nil {
		log.Info("Removed staging directory: %s", filepath.ToSlash(stagingDir))
	}
}

// CleanupDirDuring removes a staging directory during error recovery.
func CleanupDirDuring(stagingDir, phase string, log *logging.Logger) {
	if stagingDir == "" {
		return
	}
	if err := os.RemoveAll(stagingDir); err != nil && log != nil {
		log.Warn("Failed to remove staging directory %s during %s: %v", filepath.ToSlash(stagingDir), phase, err)
	}
}

// Scope manages the lifecycle of an optional staging directory.
// All methods degrade gracefully when staging is not active or the receiver is nil.
type Scope struct {
	// Dir is the staging directory path; empty when staging is not active.
	Dir string
	log *logging.Logger
}

// NewScope creates a staging directory when plan.Enabled is true.
// Returns an inactive Scope (Dir="") when staging is disabled.
func NewScope(plan Plan, pattern string, log *logging.Logger) (*Scope, error) {
	if !plan.Enabled {
		return &Scope{log: log}, nil
	}
	dir, err := CreateDir(plan.ResolvedTempDir, pattern)
	if err != nil {
		return nil, err
	}
	return &Scope{Dir: dir, log: log}, nil
}

// ActiveScope wraps an already-created staging directory in a Scope.
func ActiveScope(dir string, log *logging.Logger) *Scope {
	return &Scope{Dir: dir, log: log}
}

// ActiveDir returns Dir if staging is active, otherwise fallback. Safe to call on a nil receiver.
func (s *Scope) ActiveDir(fallback string) string {
	if s == nil || s.Dir == "" {
		return fallback
	}
	return s.Dir
}

// Cleanup removes the staging directory. Safe to call on a nil receiver or when staging is inactive.
func (s *Scope) Cleanup() {
	if s == nil || s.Dir == "" {
		return
	}
	dir := s.Dir
	s.Dir = ""
	CleanupDir(dir, s.log)
}
