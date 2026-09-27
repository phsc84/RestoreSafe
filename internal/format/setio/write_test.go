package setio

import (
	"RestoreSafe/internal/format/archive"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/security/cryptox"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var fastArgon2 = cryptox.Argon2Params{Time: cryptox.MinArgonTime, MemoryKB: cryptox.MinArgonMemoryKB, Threads: cryptox.MinArgonThreads}

func newKeySet(t *testing.T) (*container.KeySet, []byte) {
	t.Helper()
	ks, master, err := container.NewKeySet(container.AuthModePassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.AddSlot(master, container.SlotPassword, "Password", []byte("pw"), fastArgon2, nil, ""); err != nil {
		t.Fatal(err)
	}
	return ks, master
}

func TestWriteFullSetRoundTrip(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	backupDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "big.bin"), bytes.Repeat([]byte{3}, 2*1024*1024+17), 0o600); err != nil {
		t.Fatal(err)
	}
	ks, master := newKeySet(t)
	entry := naming.BackupEntry{DirectoryName: "src", ChainID: "ABC123", Date: "2026-09-26"}

	var opened []string
	res, err := Write(Params{
		SourceDir: src, OutputDir: backupDir, Entry: entry, RunID: "ABC123",
		KeySet: *ks, Master: master, SplitSizeBytes: 1024 * 1024,
		OnPartOpened: func(seq int, path string) { opened = append(opened, filepath.Base(path)) },
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(res.Parts) < 3 || len(opened) != len(res.Parts) || opened[0] != "[src]_ABC123_2026-09-26_FULL-001.enc" {
		t.Fatalf("unexpected parts: %v (opened %v)", res.Parts, opened)
	}
	if tmp, _ := catalog.ListTempParts(backupDir); len(tmp) != 0 {
		t.Fatalf("temporary parts left behind: %v", tmp)
	}

	info := catalog.InspectSet(backupDir, entry)
	if !info.Complete() {
		t.Fatalf("set not complete: %v", info.Err)
	}

	set, err := catalog.OpenSet(backupDir, entry)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	keys, _ := set.SectionKeys(master)
	m, _, err := set.ReadManifest(keys)
	if err != nil {
		t.Fatal(err)
	}
	r := archive.NewRestorer(m, t.TempDir(), true)
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(set.DecryptData(keys, pw)) }()
	if err := r.ExtractSection(pr, archive.DecideOwn(m)); err != nil {
		t.Fatalf("ExtractSection: %v", err)
	}
	if err := r.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

func TestWriteFullSetRemovesPartsOnFailure(t *testing.T) {
	t.Parallel()

	backupDir := t.TempDir()
	ks, master := newKeySet(t)
	_, err := Write(Params{
		SourceDir: filepath.Join(t.TempDir(), "does-not-exist"), OutputDir: backupDir,
		Entry: naming.BackupEntry{DirectoryName: "x", ChainID: "ABC123", Date: "2026-09-26"}, RunID: "ABC123",
		KeySet: *ks, Master: master, SplitSizeBytes: 1024 * 1024,
	})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	entries, _ := os.ReadDir(backupDir)
	if len(entries) != 0 {
		t.Fatalf("expected no files after failed backup, found %d", len(entries))
	}
}

func TestWriteRejectsInconsistentDifferentialParams(t *testing.T) {
	t.Parallel()

	ks, master := newKeySet(t)
	base := &Base{Header: &container.Header{ChainID: "ABC123", DirectoryName: "src"}}
	for _, tc := range []struct {
		entry naming.BackupEntry
		base  *Base
	}{
		{naming.BackupEntry{DirectoryName: "src", ChainID: "ABC123", Date: "2026-09-26", DiffNumber: 1}, nil},
		{naming.BackupEntry{DirectoryName: "src", ChainID: "ABC123", Date: "2026-09-26"}, base},
		{naming.BackupEntry{DirectoryName: "src", ChainID: "XYZ999", Date: "2026-09-26", DiffNumber: 1}, base},
	} {
		_, err := Write(Params{SourceDir: t.TempDir(), OutputDir: t.TempDir(), Entry: tc.entry, Base: tc.base, RunID: "RUN001", KeySet: *ks, Master: master, SplitSizeBytes: 1 << 20})
		if err == nil || !strings.Contains(err.Error(), "Internal error") {
			t.Fatalf("%+v: expected internal error, got %v", tc.entry, err)
		}
	}
}

func TestFinalizePartsRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	final := filepath.Join(dir, "[x]_ABC123_2026-09-26_FULL-001.enc")
	if err := os.WriteFile(final, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final+naming.TempSuffix, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeParts([]string{final + naming.TempSuffix}); err == nil {
		t.Fatal("expected FinalizeParts to refuse overwriting an existing part")
	}
	if data, _ := os.ReadFile(final); string(data) != "existing" {
		t.Fatal("existing part was modified")
	}
}
