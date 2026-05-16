package backup

import (
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteBackupEntryFilesRemovesPartsAndChallenge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part1 := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 1)
	part2 := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 2)
	challenge := util.ChallengeFileName(dir, entry.DirectoryName, entry.Date, entry.ID)

	createFile(t, part1, "p1")
	createFile(t, part2, "p2")
	createFile(t, challenge, "challenge")

	removed, err := deleteBackupEntryFiles(dir, entry)
	if err != nil {
		t.Fatalf("deleteBackupEntryFiles returned error: %v", err)
	}
	if removed != 3 {
		t.Fatalf("expected 3 removed files, got %d", removed)
	}

	assertNotExists(t, part1)
	assertNotExists(t, part2)
	assertNotExists(t, challenge)
}

func TestDeleteBackupEntryFilesSkipsWhenNoChallengeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("NCC001")}

	// Create only a part file, no challenge file.
	part := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 1)
	createFile(t, part, "data")

	removed, err := deleteBackupEntryFiles(dir, entry)
	if err != nil {
		t.Fatalf("expected no error when challenge file is absent, got: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed file (only the part), got %d", removed)
	}
	assertNotExists(t, part)
}

func TestDeleteOrphanLogFilesKeepsActiveRunLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	active := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	otherDate := "2026-03-13"
	otherID := util.BackupID("ZZZ999")

	activePart := util.PartFileName(dir, active.DirectoryName, active.Date, active.ID, 1)
	createFile(t, activePart, "enc")

	activeLog := util.LogFileName(dir, active.Date, active.ID)
	orphanLog := util.LogFileName(dir, otherDate, otherID)
	unrelated := filepath.Join(dir, "notes.log")
	createFile(t, activeLog, "active")
	createFile(t, orphanLog, "orphan")
	createFile(t, unrelated, "keep")

	deleted, err := deleteOrphanLogFiles(dir)
	if err != nil {
		t.Fatalf("deleteOrphanLogFiles returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected exactly 1 deleted orphan log, got %d", deleted)
	}

	assertExists(t, activeLog)
	assertNotExists(t, orphanLog)
	assertExists(t, unrelated)
}

func TestApplyRetentionPolicySkipsWhenDisabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := util.NewLogger(filepath.Join(dir, "test.log"), "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer log.Close()

	sources := []backupSource{{Resolved: dir}}
	if err := applyRetentionPolicy(dir, 0, sources, log); err != nil {
		t.Fatalf("expected no error when retention is disabled, got: %v", err)
	}
}

func TestDeleteOrphanLogFilesReturnZeroWhenTargetMissing(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nonexistent")
	deleted, err := deleteOrphanLogFiles(missing)
	if err != nil {
		t.Fatalf("expected no error for missing target dir, got: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted files, got %d", deleted)
	}
}

func TestApplyRetentionPolicySkipsWhenAllSourcesHaveErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := util.NewConsoleLogger("info")
	sources := []backupSource{
		{Resolved: dir, Err: errors.New("inaccessible")},
	}
	if err := applyRetentionPolicy(dir, 1, sources, log); err != nil {
		t.Fatalf("expected nil when directorySet is empty, got: %v", err)
	}
}

func TestApplyRetentionPolicyKeepsAllWhenBelowRetentionLimit(t *testing.T) {
	dir := t.TempDir()
	log := util.NewConsoleLogger("info")

	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ONE001")}
	part := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 1)
	createFile(t, part, "data")

	sources := []backupSource{{Resolved: dir + "/Docs", BackupName: "Docs"}}
	if err := applyRetentionPolicy(dir, 2, sources, log); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	assertExists(t, part)
}

func TestApplyRetentionPolicyDeletesOlderSetsAboveRetentionKeep(t *testing.T) {
	dir := t.TempDir()
	log := util.NewConsoleLogger("info")

	entry1 := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-13", ID: util.BackupID("OLD001")}
	entry2 := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("NEW002")}

	part1 := util.PartFileName(dir, entry1.DirectoryName, entry1.Date, entry1.ID, 1)
	part2 := util.PartFileName(dir, entry2.DirectoryName, entry2.Date, entry2.ID, 1)
	createFile(t, part1, "old data")
	createFile(t, part2, "new data")

	sources := []backupSource{{Resolved: dir + "/Docs", BackupName: "Docs"}}
	if err := applyRetentionPolicy(dir, 1, sources, log); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	assertNotExists(t, part1)
	assertExists(t, part2)
}

func createFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("failed to create parent directories: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to create file %s: %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %s (%v)", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed: %s", path)
	}
}
