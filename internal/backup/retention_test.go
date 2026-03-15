package backup

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteBackupEntryFilesRemovesPartsAndChallenge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part1 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 1)
	part2 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 2)
	challenge := util.ChallengeFileName(dir, entry.FolderName, entry.Date, entry.ID)

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

func TestDeleteOrphanLogFilesKeepsActiveRunLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	active := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	otherDate := "2026-03-13"
	otherID := util.BackupID("ZZZ999")

	activePart := util.PartFileName(dir, active.FolderName, active.Date, active.ID, 1)
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

func TestNewestPartModTimeReturnsLatest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	part1 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 1)
	part2 := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 2)

	createFile(t, part1, "p1")
	createFile(t, part2, "p2")

	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(part1, t1, t1); err != nil {
		t.Fatalf("failed to set mtime on part1: %v", err)
	}
	if err := os.Chtimes(part2, t2, t2); err != nil {
		t.Fatalf("failed to set mtime on part2: %v", err)
	}

	latest, err := newestPartModTime(dir, entry)
	if err != nil {
		t.Fatalf("newestPartModTime returned error: %v", err)
	}

	if latest.Unix() != t2.Unix() {
		t.Fatalf("expected latest mtime %v, got %v", t2, latest)
	}
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
