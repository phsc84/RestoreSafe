package operation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanLocalStagingResolvesRelativeTempDir(t *testing.T) {
	t.Parallel()
	// A relative tempDir path should be resolved to an absolute path.
	plan := PlanLocalStaging(`M:\Sources`, `M:\Backups`, "relative-temp")
	if plan.ResolvedTempDir == "relative-temp" {
		t.Fatal("expected relative tempDir to be resolved to absolute path")
	}
	if !filepath.IsAbs(plan.ResolvedTempDir) {
		t.Fatalf("expected absolute path for resolved tempDir, got: %q", plan.ResolvedTempDir)
	}
}

func TestNewStagingScopeReturnsErrorWhenDirCreationFails(t *testing.T) {
	t.Parallel()
	nonExistent := filepath.Join(t.TempDir(), "missing-parent")
	plan := LocalStagingPlan{Enabled: true, ResolvedTempDir: nonExistent}
	_, err := NewStagingScope(plan, "test-*", nil)
	if err == nil {
		t.Fatal("expected error when staging dir creation fails, got nil")
	}
}

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

func TestNewStagingScopeReturnsInactiveWhenDisabled(t *testing.T) {
	t.Parallel()
	plan := LocalStagingPlan{Enabled: false}
	scope, err := NewStagingScope(plan, "test-*", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if scope.Dir != "" {
		t.Fatalf("expected empty Dir for inactive scope, got: %q", scope.Dir)
	}
}

func TestNewStagingScopeCreatesDirectoryWhenEnabled(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	plan := LocalStagingPlan{Enabled: true, ResolvedTempDir: base}
	scope, err := NewStagingScope(plan, "test-*", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if scope.Dir == "" {
		t.Fatal("expected non-empty Dir for active scope")
	}
	if _, statErr := os.Stat(scope.Dir); statErr != nil {
		t.Fatalf("expected staging dir to exist: %v", statErr)
	}
	scope.Cleanup()
}

func TestActiveStagingScopeStoresDir(t *testing.T) {
	t.Parallel()
	scope := ActiveStagingScope("/some/dir", nil)
	if scope.Dir != "/some/dir" {
		t.Fatalf("expected Dir=/some/dir, got: %q", scope.Dir)
	}
}

func TestActiveDirReturnsFallbackWhenScopeIsNil(t *testing.T) {
	t.Parallel()
	var scope *StagingScope
	if got := scope.ActiveDir("/fallback"); got != "/fallback" {
		t.Fatalf("expected /fallback from nil scope, got: %q", got)
	}
}

func TestActiveDirReturnsFallbackWhenScopeIsInactive(t *testing.T) {
	t.Parallel()
	scope := &StagingScope{}
	if got := scope.ActiveDir("/fallback"); got != "/fallback" {
		t.Fatalf("expected /fallback from inactive scope, got: %q", got)
	}
}

func TestActiveDirReturnsDirWhenScopeIsActive(t *testing.T) {
	t.Parallel()
	scope := ActiveStagingScope("/staging/dir", nil)
	if got := scope.ActiveDir("/fallback"); got != "/staging/dir" {
		t.Fatalf("expected /staging/dir, got: %q", got)
	}
}

func TestCleanupIsNilSafe(t *testing.T) {
	t.Parallel()
	var scope *StagingScope
	scope.Cleanup() // must not panic
}

func TestCleanupRemovesStagingDir(t *testing.T) {
	t.Parallel()
	stagingDir, err := os.MkdirTemp(t.TempDir(), "scope-cleanup-*")
	if err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}
	scope := ActiveStagingScope(stagingDir, nil)
	scope.Cleanup()

	if _, statErr := os.Stat(stagingDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected staging dir removed after Cleanup, got: %v", statErr)
	}
}
