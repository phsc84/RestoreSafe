package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/testutil"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessRestorePointRestoresAndVerifies(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	set, err := catalog.OpenSet(fx.BackupDir, fx.Entry)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	if _, err := ProcessRestorePoint(set, fx.Master, "", true, nil); err != nil {
		t.Fatalf("verify: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	m, err := ProcessRestorePoint(set, fx.Master, dest, false, nil)
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

	set, err := catalog.OpenSet(fx.BackupDir, fx.Entry)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	_, err = ProcessRestorePoint(set, fx.Master, "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected corruption error, got %v", err)
	}
}
