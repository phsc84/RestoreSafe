package health

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/testutil"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// TestHealthCheckDerivesNoKeys checks that the health check, which runs at
// every start, only reads the structure of the sets: a header can ask for
// 4 GiB of Argon2 memory per key slot, which would allocate that much for
// each key derived.
func TestHealthCheckDerivesNoKeys(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	expensive := *fx.KeySet
	expensive.Slots = slices.Clone(expensive.Slots)
	for i := range expensive.Slots {
		expensive.Slots[i].KDF.Time = cryptox.MaxArgonTime
		expensive.Slots[i].KDF.MemoryKiB = cryptox.MaxArgonMemoryKB
	}
	entry := naming.BackupEntry{DirectoryName: fx.Entry.DirectoryName, ChainID: "EXP001", Date: "2026-03-15"}
	testutil.WriteFullSet(t, fx.SrcDir, fx.BackupDir, entry, &expensive, fx.Master)

	exeDir := t.TempDir()
	cfg := &config.Config{
		SourceDirectories:  []string{fx.SrcDir},
		BackupDirectory:    fx.BackupDir,
		AuthenticationMode: config.AuthModePassword,
		LogLevel:           "info",
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s := TakeSnapshot(Params{Config: cfg, ExeDir: exeDir, ConfigPath: filepath.Join(exeDir, "config.yaml"), Now: time.Now()})
	Check(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	runtime.ReadMemStats(&after)

	found := false
	for _, set := range s.Sets {
		if set.Entry == entry && set.Complete() && set.Header.KeySet.Slots[0].KDF.MemoryKiB == cryptox.MaxArgonMemoryKB {
			found = true
		}
	}
	if !found {
		t.Fatalf("the snapshot does not show the set with the expensive key set: %+v", s.Sets)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<30 {
		t.Fatalf("the health check allocated %d MiB: it derived a key", alloc>>20)
	}
}
