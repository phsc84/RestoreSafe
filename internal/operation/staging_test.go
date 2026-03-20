package operation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanLocalStagingDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		source           string
		dest             string
		tempDir          string
		expectEnabled    bool
		expectSameVolume bool
	}{
		{
			name:             "same-volume source and dest with local temp should enable staging",
			source:           `M:\Sources`,
			dest:             `M:\Backups`,
			tempDir:          `C:\Temp`,
			expectEnabled:    true,
			expectSameVolume: true,
		},
		{
			name:             "same-volume but temp on same volume should disable staging",
			source:           `M:\Sources`,
			dest:             `M:\Backups`,
			tempDir:          `M:\Temp`,
			expectEnabled:    false,
			expectSameVolume: true,
		},
		{
			name:             "different volumes should disable staging",
			source:           `M:\Sources`,
			dest:             `C:\Backups`,
			tempDir:          `C:\Temp`,
			expectEnabled:    false,
			expectSameVolume: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := PlanLocalStaging(tt.source, tt.dest, tt.tempDir)
			if plan.Enabled != tt.expectEnabled {
				t.Errorf("Expected Enabled=%v, got %v", tt.expectEnabled, plan.Enabled)
			}
			if plan.SameVolume != tt.expectSameVolume {
				t.Errorf("Expected SameVolume=%v, got %v", tt.expectSameVolume, plan.SameVolume)
			}
		})
	}
}

func TestCreateStagingDirCreatesDirectory(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	stagingDir, err := CreateStagingDir(base, "stage-*")
	if err != nil {
		t.Fatalf("CreateStagingDir failed: %v", err)
	}

	info, statErr := os.Stat(stagingDir)
	if statErr != nil {
		t.Fatalf("expected staging directory to exist: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("expected staging path to be a directory, got file: %s", stagingDir)
	}

	CleanupStagingDir(stagingDir, nil)
}

func TestCreateStagingDirReturnsRemedyErrorOnFailure(t *testing.T) {
	t.Parallel()

	nonexistentParent := filepath.Join(t.TempDir(), "missing-parent")
	_, err := CreateStagingDir(nonexistentParent, "stage-*")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Remedy:") {
		t.Fatalf("expected remedy hint in error, got: %v", err)
	}
}

func TestCleanupStagingDirRemovesDirectory(t *testing.T) {
	t.Parallel()

	stagingDir, err := os.MkdirTemp(t.TempDir(), "cleanup-*")
	if err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}

	CleanupStagingDir(stagingDir, nil)

	_, statErr := os.Stat(stagingDir)
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected staging dir removed, got stat error: %v", statErr)
	}
}

func TestCleanupStagingDirDuringRemovesDirectory(t *testing.T) {
	t.Parallel()

	stagingDir, err := os.MkdirTemp(t.TempDir(), "cleanup-during-*")
	if err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}

	CleanupStagingDirDuring(stagingDir, "error recovery", nil)

	_, statErr := os.Stat(stagingDir)
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected staging dir removed, got stat error: %v", statErr)
	}
}
