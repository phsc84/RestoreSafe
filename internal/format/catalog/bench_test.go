package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/testutil"
)

// BenchmarkInventory lists a backup directory of 500 sets.
func BenchmarkInventory(b *testing.B) {
	const sets = 500
	work := b.TempDir()
	src := filepath.Join(work, "Docs")
	backupDir := filepath.Join(work, "target")
	for _, dir := range []string{src, backupDir} {
		if err := os.Mkdir(dir, 0o750); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("restoresafe"), 0o600); err != nil {
		b.Fatal(err)
	}
	ks, master := testutil.NewPasswordKeySet(b, []byte("pw"))
	for i := range sets {
		entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: naming.BackupID(fmt.Sprintf("C%05d", i)), Date: "2026-10-08"}
		testutil.WriteFullSet(b, src, backupDir, entry, ks, master)
	}

	b.ReportAllocs()
	for b.Loop() {
		infos, err := Inventory(backupDir)
		if err != nil {
			b.Fatal(err)
		}
		if len(infos) != sets {
			b.Fatalf("Inventory found %d sets, want %d", len(infos), sets)
		}
	}
}
