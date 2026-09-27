package restorepoint

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/testutil"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openSet(t *testing.T, dir string, e naming.BackupEntry) *container.Set {
	t.Helper()
	set, err := catalog.OpenSet(dir, e)
	if err != nil {
		t.Fatalf("OpenSet %s: %v", e.String(), err)
	}
	t.Cleanup(func() { set.Close() })
	return set
}

func newDest(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	return dest
}

func TestProcessRestorePointRestoresAndVerifies(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	set := openSet(t, fx.BackupDir, fx.Entry)

	if _, err := Process(context.Background(), set, nil, fx.Master, "", true, nil, nil); err != nil {
		t.Fatalf("verify: %v", err)
	}
	dest := newDest(t)
	m, err := Process(context.Background(), set, nil, fx.Master, dest, false, nil, nil)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if m.Footer.Files != 2 {
		t.Fatalf("expected 2 files, got %d", m.Footer.Files)
	}
	want, _ := os.ReadFile(filepath.Join(fx.SrcDir, "nested", "small.txt"))
	got, err := os.ReadFile(filepath.Join(dest, "nested", "small.txt"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored content mismatch: %v", err)
	}
}

func TestProcessRestorePointDetectsCorruptedPart(t *testing.T) {
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

	set := openSet(t, fx.BackupDir, fx.Entry)
	_, err = Process(context.Background(), set, nil, fx.Master, "", true, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected corruption error, got %v", err)
	}
}

// diffFixture: a full backup, then changes (modified, new, deleted file) and
// a differential of them.
func diffFixture(t *testing.T) (*testutil.BackupFixture, naming.BackupEntry) {
	t.Helper()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "nested", "small.txt"), []byte("changed after the full backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "added.txt"), []byte("new file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fx.SrcDir, "large.bin")); err != nil {
		t.Fatal(err)
	}
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	return fx, diff
}

func TestProcessRestorePointRestoresDifferential(t *testing.T) {
	t.Parallel()

	fx, diff := diffFixture(t)
	set := openSet(t, fx.BackupDir, diff)
	base := openSet(t, fx.BackupDir, fx.Entry)
	dest := newDest(t)

	m, err := Process(context.Background(), set, base, fx.Master, dest, false, nil, nil)
	if err != nil {
		t.Fatalf("restore differential: %v", err)
	}
	if m.Footer.Files != 2 {
		t.Fatalf("expected 2 files in the restore point, got %d", m.Footer.Files)
	}
	for _, rel := range []string{"nested/small.txt", "added.txt"} {
		want, _ := os.ReadFile(filepath.Join(fx.SrcDir, filepath.FromSlash(rel)))
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: restored content differs (err %v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "large.bin")); !os.IsNotExist(err) {
		t.Fatal("a file deleted before the differential must not be restored")
	}

	// The full backup alone still restores the original state.
	fullDest := newDest(t)
	if _, err := Process(context.Background(), base, nil, fx.Master, fullDest, false, nil, nil); err != nil {
		t.Fatalf("restore full: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(fullDest, "nested", "small.txt")); string(got) != "hello restoresafe" {
		t.Fatalf("full backup restored %q", got)
	}
}

func TestProcessRestorePointRejectsWrongOrMissingBase(t *testing.T) {
	t.Parallel()

	fx, diff := diffFixture(t)
	set := openSet(t, fx.BackupDir, diff)

	if _, err := Process(context.Background(), set, nil, fx.Master, "", true, nil, nil); err == nil || !strings.Contains(err.Error(), "is required") {
		t.Fatalf("expected missing-base error, got %v", err)
	}

	other := naming.BackupEntry{DirectoryName: "Other", ChainID: "OTH001", Date: "2026-03-14"}
	fx.CreateBackupInDir(t, other)
	wrong := openSet(t, fx.BackupDir, other)
	if _, err := Process(context.Background(), set, wrong, fx.Master, "", true, nil, nil); err == nil || !strings.Contains(err.Error(), "does not belong to differential") {
		t.Fatalf("expected base mismatch error, got %v", err)
	}
}

// A full backup replaced by a different one under the same name (same chain
// ID and date) must be detected through the manifest checksum.
func TestProcessRestorePointDetectsReplacedBase(t *testing.T) {
	t.Parallel()

	fx, diff := diffFixture(t)
	parts, _ := catalog.CollectParts(fx.BackupDir, fx.Entry)
	for _, p := range parts {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	testutil.WriteFullSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, fx.KeySet, fx.Master)

	set := openSet(t, fx.BackupDir, diff)
	base := openSet(t, fx.BackupDir, fx.Entry)
	_, err := Process(context.Background(), set, base, fx.Master, "", true, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "manifest checksum mismatch") {
		t.Fatalf("expected manifest checksum error, got %v", err)
	}
}
