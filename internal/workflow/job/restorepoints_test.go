package job

import (
	"errors"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// restorePointFixture is a full backup and a differential of it.
func restorePointFixture(t *testing.T) (*testutil.BackupFixture, naming.BackupEntry, []catalog.SetInfo) {
	t.Helper()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	return fx, diff, infos
}

func infoOf(t *testing.T, infos []catalog.SetInfo, e naming.BackupEntry) catalog.SetInfo {
	t.Helper()
	for _, info := range infos {
		if info.Entry == e {
			return info
		}
	}
	t.Fatalf("%s not in the inventory", e.String())
	return catalog.SetInfo{}
}

func TestSelectionPreflightAndRows(t *testing.T) {
	t.Parallel()
	fx, diff, infos := restorePointFixture(t)
	full, d := infoOf(t, infos, fx.Entry), infoOf(t, infos, diff)
	orphan := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ZZZ999", Date: "2026-03-21", DiffNumber: 1}, SizeBytes: 5}

	items := SelectionPreflight([]catalog.SetInfo{full, d, orphan}, infos)
	if items[0].Base != nil || items[0].TotalSizeBytes != full.SizeBytes || items[0].PartCount != len(full.Parts) {
		t.Fatalf("full backup %+v", items[0])
	}
	if items[1].Base == nil || items[1].Base.Entry != fx.Entry || items[1].TotalSizeBytes != d.SizeBytes+full.SizeBytes {
		t.Fatalf("a differential is read with its full backup: %+v", items[1])
	}
	if items[2].Err == nil {
		t.Fatal("a differential without its full backup can't be used")
	}
	if got := SelectionBytes(items); got != full.SizeBytes+d.SizeBytes+full.SizeBytes {
		t.Fatalf("SelectionBytes = %d, without the unusable set", got)
	}

	rows, issues := SelectionRows(fx.BackupDir, items)
	if rows[0].Label != "Backup selection" || !strings.HasPrefix(rows[1].Text, "Path: ") || len(rows) != 5 {
		t.Fatalf("rows %+v", rows)
	}
	if len(rows[3].Details) != 1 || !strings.Contains(rows[3].Details[0], "with full backup "+fx.Entry.String()) {
		t.Fatalf("the differential's row names its full backup: %+v", rows[3])
	}
	if len(issues) != 1 || issues[0].Code != interact.CodeBaseMissing || rows[4].Status != interact.StatusError {
		t.Fatalf("issues %+v, row %+v", issues, rows[4])
	}

	if p := items[1].SetPlan(); p.Set != diff || p.Base != fx.Entry || p.Bytes != items[1].TotalSizeBytes || p.Problem != "" {
		t.Fatalf("set plan %+v", p)
	}
	if p := items[2].SetPlan(); p.Problem == "" {
		t.Fatalf("the plan says why the set can't be used: %+v", p)
	}
}

func TestEachRestorePointAndOpen(t *testing.T) {
	t.Parallel()
	fx, diff, infos := restorePointFixture(t)
	selected := []catalog.SetInfo{infoOf(t, infos, fx.Entry), infoOf(t, infos, diff)}

	var seen []string
	err := EachRestorePoint(selected, infos, func(n int, info catalog.SetInfo, base *naming.BackupEntry) error {
		set, baseSet, parts, closeAll, err := OpenRestorePoint(fx.BackupDir, info.Entry, base)
		if err != nil {
			return err
		}
		defer closeAll()
		if (base == nil) != (baseSet == nil) || parts < len(set.Paths) {
			t.Fatalf("restore point %d: base %v, parts %d", n, base, parts)
		}
		seen = append(seen, info.Entry.String())
		return nil
	})
	if err != nil || len(seen) != 2 {
		t.Fatalf("EachRestorePoint: %v, %v", seen, err)
	}

	stop := errors.New("stop")
	if err := EachRestorePoint(selected, infos, func(int, catalog.SetInfo, *naming.BackupEntry) error { return stop }); err != stop {
		t.Fatalf("the first error stops it, got %v", err)
	}
	if err := EachRestorePoint(selected[1:], selected[1:], func(int, catalog.SetInfo, *naming.BackupEntry) error { return nil }); err == nil {
		t.Fatal("a differential without its full backup fails")
	}

	// A missing full backup fails the opening, and the set is closed again.
	missing := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ZZZ999", Date: "2026-03-13"}
	if _, _, _, _, err := OpenRestorePoint(fx.BackupDir, diff, &missing); err == nil || !strings.Contains(err.Error(), "Full backup") {
		t.Fatalf("expected the full backup's error, got %v", err)
	}
	if _, _, _, _, err := OpenRestorePoint(t.TempDir(), fx.Entry, nil); err == nil {
		t.Fatal("a set that isn't there can't be opened")
	}
}

func TestLogStartNamesTheSelection(t *testing.T) {
	t.Parallel()
	fx, diff, infos := restorePointFixture(t)
	var out testutil.Output
	log := logging.NewConsoleLogger("info", &out)
	LogStart(log, "Verification", []catalog.SetInfo{infoOf(t, infos, diff), infoOf(t, infos, fx.Entry)})
	for _, want := range []string{"Verification started - ID: ", "Verification selection:", "  " + diff.String(), "  " + fx.Entry.String()} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %q", want, out.String())
		}
	}
}
