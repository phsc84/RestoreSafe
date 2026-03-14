package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectSourceFoldersReportsExpectedStatuses(t *testing.T) {
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

	statuses := inspectSourceFolders([]string{"ok", "not-a-dir.txt", "missing"}, exeDir)
	if len(statuses) != 3 {
		t.Fatalf("expected 3 source statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil {
		t.Fatalf("expected first source to be valid, got error: %v", statuses[0].Err)
	}
	if statuses[1].Err == nil {
		t.Fatal("expected file path to be rejected as non-directory")
	}
	if statuses[2].Err == nil {
		t.Fatal("expected missing path to return error")
	}
}

func TestInspectSourceFoldersAssignsAliasForDuplicateBasename(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root-b", "Documents")
	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}

	statuses := inspectSourceFolders([]string{first, second}, exeDir)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil || statuses[1].Err != nil {
		t.Fatalf("expected both sources to be valid, got errors: %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if statuses[0].BackupName == "" || statuses[1].BackupName == "" {
		t.Fatal("expected backup names to be assigned")
	}
	if statuses[0].BackupName == statuses[1].BackupName {
		t.Fatalf("expected distinct backup names for duplicate basenames, got %q", statuses[0].BackupName)
	}
}

func TestValidateSourceFolders(t *testing.T) {
	t.Parallel()

	valid := []sourceFolderStatus{{Resolved: "A"}, {Resolved: "B"}}
	if err := validateSourceFolders(valid); err != nil {
		t.Fatalf("expected valid sources to pass, got: %v", err)
	}

	invalid := []sourceFolderStatus{{Resolved: "A"}, {Resolved: "B", Err: os.ErrNotExist}}
	err := validateSourceFolders(invalid)
	if err == nil {
		t.Fatal("expected error for invalid source folders, got nil")
	}
}

func TestYesNo(t *testing.T) {
	t.Parallel()

	if got := yesNo(true); got != "enabled" {
		t.Fatalf("expected enabled, got %q", got)
	}
	if got := yesNo(false); got != "disabled" {
		t.Fatalf("expected disabled, got %q", got)
	}
}
