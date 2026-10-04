package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/plan"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// TestThroughputBenchmarkBackup times backups of a mixed dataset whose source
// and backup directory share one drive or network share. It only runs when
// RESTORESAFE_BENCH_ROOT names a directory on the volume to test;
// RESTORESAFE_BENCH_RUNS sets the number of timed runs (default 3).
func TestThroughputBenchmarkBackup(t *testing.T) {
	root, runs := benchSettings(t)
	srcDir := filepath.Join(root, "1-src-big")
	size := ensureBenchData(t, srcDir)
	t.Logf("dataset: %s (%.2f GiB)", srcDir, float64(size)/(1<<30))

	cfg := &config.Config{SplitSizeMB: config.DefaultSplitSizeMB, Argon2: testutil.FastArgon2Config}
	ks, master := testutil.NewPasswordKeySet(t, []byte("bench-pw"))
	sources := plan.ResolveSources([]string{srcDir}, "")

	// The first run warms up caches and is not counted.
	var times []time.Duration
	for run := 0; run <= runs; run++ {
		// A new name per run: a folder of an earlier run may still be
		// pending deletion on a network share.
		backupDir, err := os.MkdirTemp(root, fmt.Sprintf("backup-%d-", run))
		if err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(t.TempDir(), "bench.log")
		log, err := logging.NewLogger(logPath, "info", nil)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		var runErr error
		testutil.CaptureStdout(t, func() {
			runErr = runBackupOperation(context.Background(), &interacttest.Script{}, cfg, log, logPath, backupDir, sources, "2026-09-28", "BENCH1", ks, master, nil)
		})
		elapsed := time.Since(start)
		log.Close()
		if runErr != nil {
			t.Fatalf("backup: %v", runErr)
		}
		testutil.RemoveAll(t, backupDir)
		if run == 0 {
			t.Logf("warm-up %6.1fs", elapsed.Seconds())
			continue
		}
		times = append(times, elapsed)
		t.Logf("run %d   %6.1fs  %6.1f MiB/s", run, elapsed.Seconds(), float64(size)/(1<<20)/elapsed.Seconds())
	}
	m := median(times)
	t.Logf("SUMMARY backup median %6.1fs  %6.1f MiB/s", m.Seconds(), float64(size)/(1<<20)/m.Seconds())
}

// benchSettings reads the benchmark environment variables and skips the test
// when RESTORESAFE_BENCH_ROOT is not set.
func benchSettings(t *testing.T) (root string, runs int) {
	t.Helper()
	root = os.Getenv("RESTORESAFE_BENCH_ROOT")
	if root == "" {
		t.Skip("RESTORESAFE_BENCH_ROOT not set")
	}
	runs = 3
	if v, err := strconv.Atoi(os.Getenv("RESTORESAFE_BENCH_RUNS")); err == nil && v > 0 {
		runs = v
	}
	return root, runs
}

func median(d []time.Duration) time.Duration {
	s := slices.Clone(d)
	slices.Sort(s)
	return s[len(s)/2]
}

// ensureBenchData creates a mixed dataset of large and small incompressible
// files unless srcDir already exists, and returns its total size.
func ensureBenchData(t *testing.T, srcDir string) int64 {
	t.Helper()
	if _, err := os.Stat(srcDir); err != nil {
		rng := rand.NewChaCha8([32]byte{1})
		write := func(path string, n int) {
			buf := make([]byte, n)
			_, _ = rng.Read(buf)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, buf, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 4; i++ {
			write(filepath.Join(srcDir, "large", fmt.Sprintf("large-%d.bin", i)), 384<<20)
		}
		for i := 0; i < 2000; i++ {
			write(filepath.Join(srcDir, "small", fmt.Sprintf("d%02d", i%20), fmt.Sprintf("f%04d.bin", i)), 96<<10)
		}
	}
	var total int64
	_ = filepath.Walk(srcDir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}
