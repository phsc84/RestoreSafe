package catalog

import (
	"RestoreSafe/internal/util"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSortedEntries(t *testing.T) {
	t.Parallel()

	input := []util.BackupEntry{
		{DirectoryName: "Photos", Date: "2026-01-10", ID: util.BackupID("AAA111")},
		{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("BBB222")},
		{DirectoryName: "Archive", Date: "2026-03-14", ID: util.BackupID("CCC333")},
	}

	sorted := SortedEntries(input)

	// Sorted by date descending, then directory name ascending.
	want := []string{"Archive", "Docs", "Photos"}
	for i, name := range want {
		if sorted[i].DirectoryName != name {
			t.Fatalf("position %d: expected %q, got %q (full: %#v)", i, name, sorted[i].DirectoryName, sorted)
		}
	}

	// The original slice must not be mutated.
	if input[0].DirectoryName != "Photos" {
		t.Fatalf("expected input slice to be unchanged, got: %#v", input)
	}
}

// validChallengeJSON returns a well-formed FIDO2 challenge JSON string.
func validChallengeJSON(nopw bool) string {
	credID := base64.StdEncoding.EncodeToString(make([]byte, 64))
	salt := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return fmt.Sprintf(`{"v":1,"nopw":%v,"cred_id":%q,"salt":%q}`, nopw, credID, salt)
}

func TestScanBackupsIndexesDistinctEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part1 := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 1)
	part2 := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 2)
	if err := os.WriteFile(part1, []byte("a"), 0o600); err != nil {
		t.Fatalf("failed to write part1: %v", err)
	}
	if err := os.WriteFile(part2, []byte("b"), 0o600); err != nil {
		t.Fatalf("failed to write part2: %v", err)
	}
	// Also write an unrelated file that should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	index, err := ScanBackups(dir)
	if err != nil {
		t.Fatalf("ScanBackups returned error: %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("expected 1 distinct entry, got %d", len(index))
	}
	if index[0] != entry {
		t.Fatalf("unexpected entry: %#v", index[0])
	}
}

func TestScanBackupsReturnsEmptyForEmptyDir(t *testing.T) {
	t.Parallel()

	index, err := ScanBackups(t.TempDir())
	if err != nil {
		t.Fatalf("ScanBackups returned error: %v", err)
	}
	if len(index) != 0 {
		t.Fatalf("expected 0 entries for empty dir, got %d", len(index))
	}
}

func TestCollectPartsReturnsSortedPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Pics", Date: "2026-03-15", ID: util.BackupID("ZZZ001")}

	for seq := 3; seq >= 1; seq-- {
		path := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, seq)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("failed to write part %d: %v", seq, err)
		}
	}

	parts, err := CollectParts(dir, entry)
	if err != nil {
		t.Fatalf("CollectParts returned error: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	for i, part := range parts {
		expected := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, i+1)
		if part != expected {
			t.Fatalf("part[%d]: expected %s, got %s", i, expected, part)
		}
	}
}

func TestFindChallengeFileForRunFindsMatchingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Music", Date: "2026-03-15", ID: util.BackupID("ABC123")}
	challengePath := util.ChallengeFileName(dir, entry.DirectoryName, entry.Date, entry.ID)

	if err := os.WriteFile(challengePath, []byte("deadbeef"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	found, ok, err := FindChallengeFileForRun(dir, entry.Date, entry.ID)
	if err != nil {
		t.Fatalf("FindChallengeFileForRun returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected challenge file to be found, got not found")
	}
	if found != challengePath {
		t.Fatalf("expected path %s, got %s", challengePath, found)
	}
}

func TestFindChallengeFileForRunReturnsNotFoundWhenAbsent(t *testing.T) {
	t.Parallel()

	_, ok, err := FindChallengeFileForRun(t.TempDir(), "2026-03-15", util.BackupID("ABC123"))
	if err != nil {
		t.Fatalf("FindChallengeFileForRun returned error: %v", err)
	}
	if ok {
		t.Fatal("expected challenge file to not be found")
	}
}

func TestBackupRunUsesYubiKeyNoChallengeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-15", ID: util.BackupID("ABC123")}

	useYubiKey, yubiKeyOnly, err := BackupRunUsesYubiKey(dir, entry)
	if err != nil {
		t.Fatalf("BackupRunUsesYubiKey returned error: %v", err)
	}
	if useYubiKey {
		t.Fatal("expected no YubiKey use without challenge file")
	}
	if yubiKeyOnly {
		t.Fatal("expected yubiKeyOnly=false without challenge file")
	}
}

func TestBackupRunUsesYubiKeyDetectsPasswordMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-15", ID: util.BackupID("ABC123")}
	challengePath := util.ChallengeFileName(dir, entry.DirectoryName, entry.Date, entry.ID)

	// Challenge JSON with nopw:false means password+YubiKey mode.
	if err := os.WriteFile(challengePath, []byte(validChallengeJSON(false)), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	useYubiKey, yubiKeyOnly, err := BackupRunUsesYubiKey(dir, entry)
	if err != nil {
		t.Fatalf("BackupRunUsesYubiKey returned error: %v", err)
	}
	if !useYubiKey {
		t.Fatal("expected useYubiKey=true with challenge file present")
	}
	if yubiKeyOnly {
		t.Fatal("expected yubiKeyOnly=false for nopw:false challenge JSON")
	}
}

func TestBackupRunUsesYubiKeyDetectsYubiKeyOnlyMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-15", ID: util.BackupID("ABC123")}
	challengePath := util.ChallengeFileName(dir, entry.DirectoryName, entry.Date, entry.ID)

	// Challenge JSON with nopw:true means YubiKey-only mode.
	if err := os.WriteFile(challengePath, []byte(validChallengeJSON(true)), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	useYubiKey, yubiKeyOnly, err := BackupRunUsesYubiKey(dir, entry)
	if err != nil {
		t.Fatalf("BackupRunUsesYubiKey returned error: %v", err)
	}
	if !useYubiKey {
		t.Fatal("expected useYubiKey=true with challenge file present")
	}
	if !yubiKeyOnly {
		t.Fatal("expected yubiKeyOnly=true for nopw:true challenge JSON")
	}
}

func TestBackupRunUsesYubiKeyRejectsInvalidChallengeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-15", ID: util.BackupID("ABC123")}
	challengePath := util.ChallengeFileName(dir, entry.DirectoryName, entry.Date, entry.ID)

	if err := os.WriteFile(challengePath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	useYubiKey, _, err := BackupRunUsesYubiKey(dir, entry)
	if err == nil {
		t.Fatal("expected invalid challenge file error")
	}
	if !useYubiKey {
		t.Fatal("expected useYubiKey=true when challenge file is present")
	}
}

func TestNewestPartModTimeReturnsNewest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-15", ID: util.BackupID("ABC123")}
	part1 := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 1)
	part2 := util.PartFileName(dir, entry.DirectoryName, entry.Date, entry.ID, 2)

	if err := os.WriteFile(part1, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write part1: %v", err)
	}
	if err := os.WriteFile(part2, []byte("y"), 0o600); err != nil {
		t.Fatalf("failed to write part2: %v", err)
	}

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(part1, older, older); err != nil {
		t.Fatalf("failed to set mtime on part1: %v", err)
	}
	if err := os.Chtimes(part2, newer, newer); err != nil {
		t.Fatalf("failed to set mtime on part2: %v", err)
	}

	got, err := NewestPartModTime(dir, entry)
	if err != nil {
		t.Fatalf("NewestPartModTime returned error: %v", err)
	}
	if got.Unix() != newer.Unix() {
		t.Fatalf("expected newer mtime %v, got %v", newer, got)
	}
}

func TestNewestPartModTimeErrorsForMissingParts(t *testing.T) {
	t.Parallel()

	entry := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-15", ID: util.BackupID("ABC123")}
	_, err := NewestPartModTime(t.TempDir(), entry)
	if err == nil {
		t.Fatal("expected error for missing parts, got nil")
	}
}
