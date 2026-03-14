package engine

import (
	"os"
	"path/filepath"
	"strings"
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

	joined := strings.ToLower(statuses[0].BackupName + "|" + statuses[1].BackupName)
	if !strings.Contains(joined, "root-a") || !strings.Contains(joined, "root-b") {
		t.Fatalf("expected readable path-based aliases containing root-a/root-b, got %q and %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if !strings.Contains(joined, "-c") {
		t.Fatalf("expected aliases to include drive letter hint (e.g. -C), got %q and %q", statuses[0].BackupName, statuses[1].BackupName)
	}
}

func TestInspectSourceFoldersWarnsOnTrueIdenticalDuplicate(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	shared := filepath.Join(exeDir, "root-a", "Documents")
	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatalf("failed to create shared source: %v", err)
	}

	statuses := inspectSourceFolders([]string{shared, shared}, exeDir)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil || statuses[1].Err != nil {
		t.Fatalf("expected both statuses to be valid, got %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if statuses[0].Skip {
		t.Fatal("expected first source to stay active")
	}
	if !statuses[1].Skip {
		t.Fatal("expected second identical source to be skipped")
	}
	if statuses[1].Warning == "" || !strings.Contains(strings.ToLower(statuses[1].Warning), "identical duplicate") {
		t.Fatalf("expected duplicate warning, got %q", statuses[1].Warning)
	}
	if statuses[0].BackupName == "" || statuses[1].BackupName == "" {
		t.Fatalf("expected backup names to be assigned, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if statuses[0].BackupName != statuses[1].BackupName {
		t.Fatalf("expected identical duplicates to share backup name, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if strings.Contains(statuses[1].BackupName, "-2") {
		t.Fatalf("expected no numeric suffix for true identical duplicate, got %q", statuses[1].BackupName)
	}
}

func TestInspectSourceFoldersDistinguishesHyphenAndUnderscoreAliases(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root_a", "Documents")
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
		t.Fatalf("expected both sources to be valid, got %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if statuses[0].Skip || statuses[1].Skip {
		t.Fatal("expected non-identical sources to remain active (not skipped)")
	}

	firstName := statuses[0].BackupName
	secondName := statuses[1].BackupName
	if firstName == "" || secondName == "" {
		t.Fatalf("expected backup names to be populated, got %q / %q", firstName, secondName)
	}
	if firstName == secondName {
		t.Fatalf("expected distinct aliases, got %q and %q", firstName, secondName)
	}
	firstLower := strings.ToLower(firstName)
	secondLower := strings.ToLower(secondName)
	if !(strings.Contains(firstLower, "root-a-c") || strings.Contains(secondLower, "root-a-c")) {
		t.Fatalf("expected one alias to include root-a-c, got %q / %q", firstName, secondName)
	}
	if !(strings.Contains(firstLower, "root_a-c") || strings.Contains(secondLower, "root_a-c")) {
		t.Fatalf("expected one alias to include root_a-c, got %q / %q", firstName, secondName)
	}
	if strings.Contains(firstName, "-2") || strings.Contains(secondName, "-2") {
		t.Fatalf("expected no numeric suffix fallback, got %q / %q", firstName, secondName)
	}
}

func TestInspectSourceFoldersDistinguishesSpaceFromHyphenAndUnderscoreAliases(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root_a", "Documents")
	third := filepath.Join(exeDir, "root a", "Documents")
	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}
	if err := os.MkdirAll(third, 0o750); err != nil {
		t.Fatalf("failed to create third source: %v", err)
	}

	statuses := inspectSourceFolders([]string{first, second, third}, exeDir)
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	for i := range statuses {
		if statuses[i].Err != nil {
			t.Fatalf("expected all sources to be valid, got status[%d] error: %v", i, statuses[i].Err)
		}
		if statuses[i].Skip {
			t.Fatalf("expected all sources to remain active, status[%d] was skipped", i)
		}
	}

	joined := strings.ToLower(statuses[0].BackupName + "|" + statuses[1].BackupName + "|" + statuses[2].BackupName)
	if !strings.Contains(joined, "root-a-c") {
		t.Fatalf("expected alias for root-a to contain root-a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root_a-c") {
		t.Fatalf("expected alias for root_a to contain root_a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~a-c") {
		t.Fatalf("expected alias for root a to contain root~a-c, got %q", joined)
	}
}

func TestInspectSourceFoldersFailsOnNonIdenticalAliasCollision(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root.a", "Documents")
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

	if statuses[0].Skip || statuses[1].Skip {
		t.Fatal("expected non-identical sources to remain active (not skipped)")
	}
	if statuses[0].Err == nil || statuses[1].Err == nil {
		t.Fatalf("expected both sources to fail on alias collision, got %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if !strings.Contains(strings.ToLower(statuses[0].Err.Error()), "alias collision") {
		t.Fatalf("expected alias collision error, got %v", statuses[0].Err)
	}
	if !strings.Contains(strings.ToLower(statuses[1].Err.Error()), "alias collision") {
		t.Fatalf("expected alias collision error, got %v", statuses[1].Err)
	}
	if statuses[0].BackupName == "" || statuses[1].BackupName == "" {
		t.Fatalf("expected colliding backup names to be populated, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if statuses[0].BackupName != statuses[1].BackupName {
		t.Fatalf("expected both entries to report the same colliding name, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if strings.Contains(statuses[0].BackupName, "-2") || strings.Contains(statuses[1].BackupName, "-2") {
		t.Fatalf("expected no numeric suffix fallback, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
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
