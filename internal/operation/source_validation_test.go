package operation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectSourceFoldersForValidationReportsDirectoryErrors(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	okDir := filepath.Join(exeDir, "ok")
	if err := os.MkdirAll(okDir, 0o750); err != nil {
		t.Fatalf("failed to create ok directory: %v", err)
	}
	filePath := filepath.Join(exeDir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create file path: %v", err)
	}

	statuses := InspectSourceFoldersForValidation([]string{"ok", "not-a-dir.txt", "missing"}, exeDir)
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}
	if statuses[0].Err != nil {
		t.Fatalf("expected first source to be valid, got error: %v", statuses[0].Err)
	}
	if statuses[1].Err == nil {
		t.Fatal("expected non-directory path to fail")
	}
	if statuses[2].Err == nil {
		t.Fatal("expected missing path to fail")
	}
}

func TestInspectSourceFoldersForValidationMarksIdenticalDuplicates(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	shared := filepath.Join(exeDir, "Docs")
	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatalf("failed to create shared folder: %v", err)
	}

	statuses := InspectSourceFoldersForValidation([]string{shared, shared}, exeDir)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0].Skip {
		t.Fatal("expected first status to remain active")
	}
	if !statuses[1].Skip {
		t.Fatal("expected duplicate status to be skipped")
	}
	if !strings.Contains(strings.ToLower(statuses[1].Warning), "identical duplicate") {
		t.Fatalf("expected duplicate warning, got %q", statuses[1].Warning)
	}
}
