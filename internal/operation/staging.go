package operation

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
)

// LocalStagingPlan describes whether and how to use local staging to avoid same-volume contention.
type LocalStagingPlan struct {
	Enabled          bool
	SameVolume       bool
	ResolvedTempDir  string
	TempSharesVolume bool
}

// PlanLocalStaging determines if local staging should be used based on source, destination, and TEMP volumes.
// It stages to local TEMP when source and dest are on the same volume AND TEMP is on a different volume.
func PlanLocalStaging(sourceDir, destDir, tempDir string) LocalStagingPlan {
	resolvedTempDir := tempDir
	if resolvedTempDir != "" && !filepath.IsAbs(resolvedTempDir) {
		if absPath, err := filepath.Abs(resolvedTempDir); err == nil {
			resolvedTempDir = absPath
		}
	}

	sameVolume := util.SameVolume(sourceDir, destDir)
	tempSharesVolume := resolvedTempDir != "" && util.SameVolume(sourceDir, resolvedTempDir)

	return LocalStagingPlan{
		Enabled:          sameVolume && resolvedTempDir != "" && !tempSharesVolume,
		SameVolume:       sameVolume,
		ResolvedTempDir:  resolvedTempDir,
		TempSharesVolume: tempSharesVolume,
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

// StagingScope manages the lifecycle of an optional staging directory.
// All methods degrade gracefully when staging is not active or the receiver is nil.
type StagingScope struct {
	// Dir is the staging directory path; empty when staging is not active.
	Dir string
	log *util.Logger
}

// NewStagingScope creates a staging directory when plan.Enabled is true.
// Returns an inactive StagingScope (Dir="") when staging is disabled.
func NewStagingScope(plan LocalStagingPlan, pattern string, log *util.Logger) (*StagingScope, error) {
	if !plan.Enabled {
		return &StagingScope{log: log}, nil
	}
	dir, err := CreateStagingDir(plan.ResolvedTempDir, pattern)
	if err != nil {
		return nil, err
	}
	return &StagingScope{Dir: dir, log: log}, nil
}

// ActiveStagingScope wraps an already-created staging directory in a StagingScope.
func ActiveStagingScope(dir string, log *util.Logger) *StagingScope {
	return &StagingScope{Dir: dir, log: log}
}

// ActiveDir returns Dir if staging is active, otherwise fallback. Safe to call on a nil receiver.
func (s *StagingScope) ActiveDir(fallback string) string {
	if s == nil || s.Dir == "" {
		return fallback
	}
	return s.Dir
}

// Cleanup removes the staging directory. Safe to call on a nil receiver or when staging is inactive.
func (s *StagingScope) Cleanup() {
	if s == nil {
		return
	}
	CleanupStagingDir(s.Dir, s.log)
}

// CleanupDuring removes the staging directory during error recovery, tagging the phase in the log.
// Safe to call on a nil receiver.
func (s *StagingScope) CleanupDuring(phase string) {
	if s == nil {
		return
	}
	CleanupStagingDirDuring(s.Dir, phase, s.log)
}
