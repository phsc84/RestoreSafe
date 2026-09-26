package catalog

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePart(t *testing.T, dir string, e util.BackupEntry, seq int) string {
	t.Helper()
	p := util.PartFileName(dir, e, seq)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanBackupsIndexesDistinctEntriesAndIgnoresOtherFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	a := util.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-15"}
	b := util.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20", DiffNumber: 1}
	writePart(t, dir, a, 1)
	writePart(t, dir, a, 2)
	writePart(t, dir, b, 1)
	for _, other := range []string{"[Docs]_2026-03-15_ABC123-001.enc", "notes.txt", "[Docs]_ABC123_2026-03-15_FULL-003.enc.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, other), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ScanBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %v", entries)
	}
}

func TestCollectPartsReturnsSortedPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	e := util.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-15"}
	p3 := writePart(t, dir, e, 3)
	p1 := writePart(t, dir, e, 1)
	p2 := writePart(t, dir, e, 2)
	parts, err := CollectParts(dir, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || parts[0] != p1 || parts[1] != p2 || parts[2] != p3 {
		t.Fatalf("unexpected order: %v", parts)
	}
}

func TestInventoryReportsCompleteAndIncompleteSets(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	// A set with only a stray part is incomplete.
	broken := util.BackupEntry{DirectoryName: "Other", ChainID: "ZZZ999", Date: "2026-03-15"}
	writePart(t, fx.BackupDir, broken, 1)

	infos, err := Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(infos))
	}
	var complete, incomplete int
	for _, info := range infos {
		if info.Complete() {
			complete++
			if info.Header.RunID != "FIX001" || info.SizeBytes == 0 {
				t.Fatalf("unexpected complete info: %+v", info)
			}
		} else {
			incomplete++
		}
	}
	if complete != 1 || incomplete != 1 {
		t.Fatalf("complete=%d incomplete=%d", complete, incomplete)
	}
	if ks := CurrentKeySet(infos); ks == nil || ks.ID != fx.KeySet.ID {
		t.Fatal("CurrentKeySet must return the key set of the complete set")
	}
}

func TestInventorySortsNewestFirst(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	newer := util.BackupEntry{DirectoryName: "Second", ChainID: "NEW001", Date: "2026-03-20"}
	fx.CreateBackupInDir(t, newer)

	infos, err := Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	// Both sets may share the same second; the newer one must never sort last
	// when its timestamp is strictly greater.
	if infos[0].Created().Before(infos[1].Created()) {
		t.Fatal("inventory is not sorted newest first")
	}
}

func TestOpenSetRejectsRenamedFiles(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	parts, _ := CollectParts(fx.BackupDir, fx.Entry)
	renamed := fx.Entry
	renamed.Date = "2026-01-01"
	for i, p := range parts {
		if err := os.Rename(p, util.PartFileName(fx.BackupDir, renamed, i+1)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := OpenSet(fx.BackupDir, renamed)
	if err == nil || !strings.Contains(err.Error(), "does not match the backup header") {
		t.Fatalf("expected name/header mismatch, got %v", err)
	}
}

func TestOpenSetDetectsMissingMiddlePart(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	if fx.Parts < 3 {
		t.Fatalf("fixture needs at least 3 parts, has %d", fx.Parts)
	}
	parts, _ := CollectParts(fx.BackupDir, fx.Entry)
	if err := os.Remove(parts[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSet(fx.BackupDir, fx.Entry); err == nil || !strings.Contains(err.Error(), "Missing part file 002") {
		t.Fatalf("expected missing part error, got %v", err)
	}
}

func TestIsIncomplete(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	parts, _ := CollectParts(fx.BackupDir, fx.Entry)
	last := parts[len(parts)-1]
	fi, _ := os.Stat(last)
	if err := os.Truncate(last, fi.Size()-5); err != nil {
		t.Fatal(err)
	}
	info := InspectSet(fx.BackupDir, fx.Entry)
	if info.Complete() || !IsIncomplete(info.Err) {
		t.Fatalf("expected incomplete set, got %v", info.Err)
	}
}

func TestListTempPartsAndLegacyFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{
		"[Docs]_ABC123_2026-03-15_FULL-001.enc.tmp",
		"[Docs]_2026-03-15_ABC123-001.enc",
		"[Docs]_2026-03-15_ABC123.challenge",
		"[Docs]_ABC123_2026-03-15_FULL-001.enc",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	temps, _ := ListTempParts(dir)
	legacy, _ := ListLegacyFiles(dir)
	if len(temps) != 1 || len(legacy) != 2 {
		t.Fatalf("temps=%v legacy=%v", temps, legacy)
	}
}

func TestSelectInfosKeepsSelectionOrder(t *testing.T) {
	t.Parallel()

	a := SetInfo{Entry: util.BackupEntry{DirectoryName: "A", ChainID: "ABC123", Date: "2026-03-15"}}
	b := SetInfo{Entry: util.BackupEntry{DirectoryName: "B", ChainID: "ABC123", Date: "2026-03-15"}}
	got := SelectInfos([]SetInfo{a, b}, []util.BackupEntry{b.Entry, a.Entry, {DirectoryName: "missing"}})
	if len(got) != 2 || got[0].Entry != b.Entry || got[1].Entry != a.Entry {
		t.Fatalf("unexpected selection: %+v", got)
	}
}
