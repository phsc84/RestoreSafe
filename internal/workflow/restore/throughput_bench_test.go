package restore

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/format/setwriter"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/unlock"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// TestThroughputBenchmarkRestore times restores of a backup set into a
// directory on the same drive or network share. It uses the dataset the backup
// benchmark created in RESTORESAFE_BENCH_ROOT (see
// backup.TestThroughputBenchmarkBackup for the variables).
func TestThroughputBenchmarkRestore(t *testing.T) {
	root := os.Getenv("RESTORESAFE_BENCH_ROOT")
	if root == "" {
		t.Skip("RESTORESAFE_BENCH_ROOT not set")
	}
	runs := 3
	if v, err := strconv.Atoi(os.Getenv("RESTORESAFE_BENCH_RUNS")); err == nil && v > 0 {
		runs = v
	}
	srcDir := filepath.Join(root, "src")
	if _, err := os.Stat(srcDir); err != nil {
		t.Fatalf("dataset missing, run TestThroughputBenchmarkBackup first: %v", err)
	}

	// The set to restore lives on the tested volume; writing it is not timed.
	backupDir := filepath.Join(root, "restore-set")
	_ = os.RemoveAll(backupDir)
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(backupDir)
	ks, master := testutil.NewPasswordKeySet(t, []byte("bench-pw"))
	entry := naming.BackupEntry{DirectoryName: "src", ChainID: "BENCH2", Date: "2026-09-28"}
	if _, err := setwriter.Write(setwriter.Params{
		SourceDir:      srcDir,
		OutputDir:      backupDir,
		Entry:          entry,
		RunID:          entry.ChainID,
		KeySet:         *ks,
		Master:         master,
		SplitSizeBytes: config.DefaultSplitSizeMB << 20,
	}); err != nil {
		t.Fatal(err)
	}
	infos := fixtureInfos(t, &testutil.BackupFixture{BackupDir: backupDir})
	var size int64
	for _, info := range infos {
		if !info.Complete() {
			t.Fatalf("set %s incomplete: %v", info.Entry.String(), info.Err)
		}
		size += info.SizeBytes
	}
	t.Logf("set: %.2f GiB", float64(size)/(1<<30))

	// The first run warms up caches and is not counted.
	var times []time.Duration
	for run := 0; run <= runs; run++ {
		restoreRoot := filepath.Join(root, fmt.Sprintf("restore-%d", run))
		masters := unlock.MasterKeys{ks.ID: append([]byte(nil), master...)}
		start := time.Now()
		var err error
		testutil.CaptureStdout(t, func() {
			_, err = restoreSelectedEntries(context.Background(), nil, infos, infos, backupDir, restoreRoot, masters, logging.NewConsoleLogger("info", nil))
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		if err := os.RemoveAll(restoreRoot); err != nil {
			t.Logf("cleanup %s: %v", restoreRoot, err)
		}
		if run == 0 {
			t.Logf("warm-up %6.1fs", elapsed.Seconds())
			continue
		}
		times = append(times, elapsed)
		t.Logf("run %d   %6.1fs  %6.1f MiB/s", run, elapsed.Seconds(), float64(size)/(1<<20)/elapsed.Seconds())
	}
	slices.Sort(times)
	m := times[len(times)/2]
	t.Logf("SUMMARY restore median %6.1fs  %6.1f MiB/s", m.Seconds(), float64(size)/(1<<20)/m.Seconds())
}
