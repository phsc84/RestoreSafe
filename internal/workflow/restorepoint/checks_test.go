package restorepoint

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/manifest"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
	"github.com/phsc84/restoresafe/internal/testutil"
)

func TestVerifyOwnDataChecksTheSetsOwnFiles(t *testing.T) {
	t.Parallel()

	fx, diff := diffFixture(t)
	for _, e := range []struct {
		name  string
		set   *container.Set
		files int
	}{
		{"full", openSet(t, fx.BackupDir, fx.Entry), 2},
		// The differential's own section holds the changed and the new file;
		// its full backup is not read.
		{"differential", openSet(t, fx.BackupDir, diff), 2},
	} {
		m, err := VerifyOwnData(context.Background(), e.set, fx.Master, Output{})
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if m.Footer.Files != e.files {
			t.Fatalf("%s: %d files, want %d", e.name, m.Footer.Files, e.files)
		}
	}
}

func TestVerifyOwnDataDetectsCorruptedPart(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	parts, _ := catalog.CollectParts(fx.BackupDir, fx.Entry)
	data, err := os.ReadFile(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(parts[1], data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = VerifyOwnData(context.Background(), openSet(t, fx.BackupDir, fx.Entry), fx.Master, Output{})
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected corruption error, got %v", err)
	}
}

func TestProcessAndVerifyOwnDataRejectWrongKey(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	set := openSet(t, fx.BackupDir, fx.Entry)
	wrong, _ := cryptox.RandomBytes(cryptox.KeyLen)
	if _, err := Verify(context.Background(), set, nil, wrong, Output{}); err == nil {
		t.Fatal("Process must fail with a wrong key")
	}
	if _, err := VerifyOwnData(context.Background(), set, wrong, Output{}); err == nil {
		t.Fatal("VerifyOwnData must fail with a wrong key")
	}
}

func TestProcessStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Verify(ctx, openSet(t, fx.BackupDir, fx.Entry), nil, fx.Master, Output{}); err == nil {
		t.Fatal("a cancelled Process must fail")
	}
}

func TestCheckBaseLinkRejectsEveryMismatch(t *testing.T) {
	t.Parallel()

	diff := &container.Header{SetType: manifest.SetTypeDiff, ChainID: "ABC123", DirectoryName: "Docs", DiffNumber: 2, BaseDate: "2026-03-14", BaseManifestSHA256: "sum", KeySet: container.KeySet{ID: "keys"}}
	base := func(change func(*container.Header)) *container.Header {
		h := &container.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "Docs", Date: "2026-03-14", KeySet: container.KeySet{ID: "keys"}}
		if change != nil {
			change(h)
		}
		return h
	}
	if err := checkBaseLink(diff, base(nil), "sum"); err != nil {
		t.Fatalf("matching base rejected: %v", err)
	}
	for _, c := range []struct {
		name string
		base *container.Header
		sum  string
		want string
	}{
		{"differential as base", base(func(h *container.Header) { h.SetType = manifest.SetTypeDiff }), "sum", "is not a full backup"},
		{"other chain", base(func(h *container.Header) { h.ChainID = "XYZ999" }), "sum", "does not belong to differential 002"},
		{"other folder", base(func(h *container.Header) { h.DirectoryName = "Mail" }), "sum", "does not belong"},
		{"other date", base(func(h *container.Header) { h.Date = "2026-03-15" }), "sum", "does not belong"},
		{"other keys", base(func(h *container.Header) { h.KeySet.ID = "other" }), "sum", "uses different keys"},
		{"other manifest", base(nil), "other", "manifest checksum mismatch"},
	} {
		if err := checkBaseLink(diff, c.base, c.sum); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: expected %q, got %v", c.name, c.want, err)
		}
	}
}

func TestReportSkippedAndStaleFiles(t *testing.T) {
	t.Parallel()

	m := &manifest.Manifest{Entries: []manifest.Entry{
		{Path: "ok.txt", Type: manifest.TypeFile},
		{Path: "Mail/archive.pst", Type: manifest.TypeSkipped},
		{Path: "busy.db", Type: manifest.TypeFile, Stale: true},
	}}
	var out testutil.Output
	log := logging.NewConsoleLogger("info", &out)
	if n := ReportSkippedFiles(m, "Docs", log); n != 1 {
		t.Fatalf("ReportSkippedFiles = %d, want 1", n)
	}
	if n := ReportStaleFiles(m, "Docs", log); n != 1 {
		t.Fatalf("ReportStaleFiles = %d, want 1", n)
	}
	for _, want := range []string{"[Docs] 1 file(s) are not in this backup", "Mail/archive.pst", "older version of the full backup", "busy.db"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %q", want, out.String())
		}
	}

	clean := &manifest.Manifest{Entries: []manifest.Entry{{Path: "ok.txt", Type: manifest.TypeFile}}}
	var quiet testutil.Output
	log = logging.NewConsoleLogger("info", &quiet)
	if ReportSkippedFiles(clean, "Docs", log) != 0 || ReportStaleFiles(clean, "Docs", log) != 0 || quiet.String() != "" {
		t.Fatalf("a clean manifest reports nothing, got %q", quiet.String())
	}
}

func TestSectionSizeAddsTheFullBackup(t *testing.T) {
	t.Parallel()

	set := &container.Set{Trailer: container.Trailer{DataLength: 300}}
	base := &container.Set{Trailer: container.Trailer{DataLength: 1000}}
	if got := SectionSize(set, nil); got != 300 {
		t.Fatalf("without base: %d", got)
	}
	if got := SectionSize(set, base); got != 1300 {
		t.Fatalf("with base: %d", got)
	}
}
