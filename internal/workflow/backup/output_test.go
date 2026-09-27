package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCopyFileWithCountersReturnsErrorForMissingSource(t *testing.T) {
	t.Parallel()
	var in, out, calls atomic.Int64
	err := copyFileWithCounters(context.Background(), filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst"), &in, &out, &calls)
	if err == nil || !strings.Contains(err.Error(), "Failed to open source file") {
		t.Fatalf("expected open error, got %v", err)
	}
}

func TestCopyFileWithCountersReturnsErrorForBadDestination(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src")
	createFile(t, src, "data")
	var in, out, calls atomic.Int64
	err := copyFileWithCounters(context.Background(), src, filepath.Join(t.TempDir(), "missing-dir", "dst"), &in, &out, &calls)
	if err == nil || !strings.Contains(err.Error(), "Failed to create destination file") {
		t.Fatalf("expected create error, got %v", err)
	}
}

func TestMoveBackupResultsMovesOnlyPartsAndFinalizesThem(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	stagingDir := fx.BackupDir
	target := t.TempDir()
	createFile(t, filepath.Join(stagingDir, "unrelated.txt"), "not a part")

	output := testutil.CaptureStdout(t, func() {
		if err := moveBackupResults(context.Background(), nil, stagingDir, target, nil, nil, logging.NewConsoleLogger("info", nil)); err != nil {
			t.Fatalf("moveBackupResults: %v", err)
		}
	})

	if !catalog.InspectSet(target, fx.Entry).Complete() {
		t.Fatal("moved set is not complete")
	}
	if _, err := os.Stat(filepath.Join(target, "unrelated.txt")); !os.IsNotExist(err) {
		t.Fatal("non-part file must not be moved")
	}
	if temps, _ := catalog.ListTempParts(target); len(temps) != 0 {
		t.Fatalf("temporary parts left behind: %v", temps)
	}
	moveIdx := strings.Index(output, "Move: ")
	summaryIdx := strings.Index(output, "successfully moved")
	if moveIdx < 0 || summaryIdx < moveIdx {
		t.Fatalf("expected per-file lines before the summary, got %q", output)
	}
}

func TestMoveBackupResultsFailsWhenStagingDirMissing(t *testing.T) {
	t.Parallel()
	err := moveBackupResults(context.Background(), nil, filepath.Join(t.TempDir(), "missing"), t.TempDir(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Failed to list staging directory") {
		t.Fatalf("expected listing error, got %v", err)
	}
}
