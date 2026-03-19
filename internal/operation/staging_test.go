package operation

import (
	"RestoreSafe/internal/testutil"
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

func TestCopyFileCopiesContent(t *testing.T) {
	t.Parallel()

	// Create temp directory for test files
	tempDir := t.TempDir()
	srcFile := filepath.Join(tempDir, "source.txt")
	dstFile := filepath.Join(tempDir, "dest.txt")

	// Write test content to source
	testContent := []byte("test data for copying")
	if err := os.WriteFile(srcFile, testContent, 0o600); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// Copy file
	if err := CopyFile(srcFile, dstFile); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify destination exists and has same content
	if _, err := os.Stat(dstFile); err != nil {
		t.Fatalf("Destination file does not exist: %v", err)
	}

	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(testContent) {
		t.Errorf("Expected content %q, got %q", testContent, dstContent)
	}
}

func TestCopyFileOverwritesDestination(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	srcFile := filepath.Join(tempDir, "source.txt")
	dstFile := filepath.Join(tempDir, "dest.txt")

	// Write source
	if err := os.WriteFile(srcFile, []byte("new content"), 0o600); err != nil {
		t.Fatalf("Failed to write source: %v", err)
	}

	// Write existing destination
	if err := os.WriteFile(dstFile, []byte("old content"), 0o600); err != nil {
		t.Fatalf("Failed to write destination: %v", err)
	}

	// Copy should overwrite
	if err := CopyFile(srcFile, dstFile); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify overwritten
	content, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}

	if string(content) != "new content" {
		t.Errorf("Expected overwritten content, got %q", content)
	}
}

func TestStageLocalDirectoryStructure(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping staging integration test in CI")
	}

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	stagingBase := filepath.Join(tempDir, "staging")
	if err := os.MkdirAll(stagingBase, 0o750); err != nil {
		t.Fatalf("Failed to create staging base directory: %v", err)
	}

	// Create source structure
	if err := os.MkdirAll(filepath.Join(sourceDir, "subdir"), 0o750); err != nil {
		t.Fatalf("Failed to create source subdir: %v", err)
	}

	// Create test files
	files := map[string]string{
		"file1.txt":        "content1",
		"subdir/file2.txt": "content2",
		"subdir/file3.txt": "content3",
	}

	for path, content := range files {
		fullPath := filepath.Join(sourceDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatalf("Failed to write %s: %v", path, err)
		}
	}

	// Stage directory
	stagedDir, err := testutil.StageLocalDirectory(sourceDir, stagingBase)
	if err != nil {
		t.Fatalf("testutil.StageLocalDirectory failed: %v", err)
	}
	defer os.RemoveAll(stagedDir)

	// Verify structure
	for path, expectedContent := range files {
		stagedPath := filepath.Join(stagedDir, path)
		if _, err := os.Stat(stagedPath); err != nil {
			t.Errorf("Staged file not found: %s", path)
			continue
		}

		content, err := os.ReadFile(stagedPath)
		if err != nil {
			t.Errorf("Failed to read staged file %s: %v", path, err)
			continue
		}

		if string(content) != expectedContent {
			t.Errorf("File %s has wrong content: expected %q, got %q", path, expectedContent, content)
		}
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
