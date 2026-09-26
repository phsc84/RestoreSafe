package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureInfos(t *testing.T, fx *testutil.BackupFixture) []catalog.SetInfo {
	t.Helper()
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	return infos
}

func masterKeys(fx *testutil.BackupFixture) operation.MasterKeys {
	return operation.MasterKeys{fx.KeySet.ID: append([]byte(nil), fx.Master...)}
}

// assertTreesEqual compares file contents and directory structure.
func assertTreesEqual(t *testing.T, want, got string) {
	t.Helper()
	err := filepath.WalkDir(want, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(want, path)
		other := filepath.Join(got, rel)
		if d.IsDir() {
			if fi, err := os.Stat(other); err != nil || !fi.IsDir() {
				return fmt.Errorf("directory %s missing in restore", rel)
			}
			return nil
		}
		a, _ := os.ReadFile(path)
		b, err := os.ReadFile(other)
		if err != nil || !bytes.Equal(a, b) {
			return fmt.Errorf("file %s differs after restore", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunRestoreOperationRestoresFixture(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("restore-pw"))
	infos := fixtureInfos(t, fx.BackupFixture)
	logPath := filepath.Join(t.TempDir(), "restore.log")
	log, _ := util.NewLogger(logPath, "info", nil)

	var err error
	output := testutil.CaptureStdout(t, func() {
		err = runRestoreOperation(context.Background(), &ui.Console{}, infos, infos, fx.BackupDir, fx.RestoreRoot, logPath, masterKeys(fx.BackupFixture), log, operation.LocalStagingPlan{}, 0)
	})
	log.Close()
	if err != nil {
		t.Fatalf("runRestoreOperation: %v", err)
	}
	if !strings.Contains(output, "Restore completed successfully.") || !strings.Contains(output, "successfully restored and checked") {
		t.Fatalf("unexpected output: %q", output)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
}

func TestRestoreSelectedEntriesWithStagingRoundTrip(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("staging-pw"))
	infos := fixtureInfos(t, fx.BackupFixture)
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: t.TempDir()}

	var err error
	testutil.CaptureStdout(t, func() {
		_, err = restoreSelectedEntries(context.Background(), nil, infos, infos, fx.BackupDir, fx.RestoreRoot, masterKeys(fx.BackupFixture), util.NewConsoleLogger("info", nil), plan)
	})
	if err != nil {
		t.Fatalf("restore with staging: %v", err)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
	if entries, _ := os.ReadDir(plan.ResolvedTempDir); len(entries) != 0 {
		t.Fatalf("staging directory not cleaned up: %d entries left", len(entries))
	}
}

func TestRestoreDifferentialWithStaging(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "nested", "small.txt"), []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := fixtureInfos(t, fx.BackupFixture)
	selected := catalog.SelectInfos(infos, []util.BackupEntry{diff})
	plan := operation.LocalStagingPlan{Enabled: true, ResolvedTempDir: t.TempDir()}

	var err error
	out := testutil.CaptureStdout(t, func() {
		_, err = restoreSelectedEntries(context.Background(), nil, selected, infos, fx.BackupDir, fx.RestoreRoot, masterKeys(fx.BackupFixture), util.NewConsoleLogger("info", nil), plan)
	})
	if err != nil {
		t.Fatalf("restore differential with staging: %v", err)
	}
	if !strings.Contains(out, "Copying backup files of "+diff.String()) || !strings.Contains(out, "Copying backup files of "+fx.Entry.String()) {
		t.Fatalf("expected differential and full backup to be staged: %q", out)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
}

func TestRestoreEntryRejectsWrongKey(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("right"))
	wrong, _ := security.RandomBytes(security.KeyLen)

	var err error
	testutil.CaptureStdout(t, func() {
		_, err = restoreEntry(context.Background(), nil, fx.Entry, nil, fx.BackupDir, fx.RestoreRoot, wrong, util.NewConsoleLogger("info", nil))
	})
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}

func TestRestoreEntryRefusesExistingDestination(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	if err := os.Mkdir(filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName), 0o750); err != nil {
		t.Fatal(err)
	}
	var err error
	testutil.CaptureStdout(t, func() {
		_, err = restoreEntry(context.Background(), nil, fx.Entry, nil, fx.BackupDir, fx.RestoreRoot, fx.Master, util.NewConsoleLogger("info", nil))
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-destination error, got %v", err)
	}
}

func TestRestoreEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	entry := util.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}
	_, err := restoreEntry(context.Background(), nil, entry, nil, t.TempDir(), t.TempDir(), make([]byte, 32), util.NewConsoleLogger("info", nil))
	if err == nil || !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got %v", err)
	}
}

func TestStageBackupEntryLocallyCopiesParts(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	var stageDir string
	var err error
	testutil.CaptureStdout(t, func() {
		stageDir, err = stageBackupEntriesLocally(context.Background(), nil, fx.BackupDir, []util.BackupEntry{fx.Entry}, t.TempDir(), util.NewConsoleLogger("info", nil))
	})
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.InspectSet(stageDir, fx.Entry).Complete() {
		t.Fatal("staged set is not complete")
	}
}

func TestRunReturnsNilWhenNoBackupsFound(t *testing.T) {
	t.Parallel()
	cfg := &util.Config{BackupDirectory: t.TempDir()}
	output := testutil.CaptureStdout(t, func() {
		if err := Run(context.Background(), &ui.Console{}, cfg, ""); err != nil {
			t.Errorf("expected nil for empty target dir, got: %v", err)
		}
	})
	if !strings.Contains(output, "No complete backups found") {
		t.Fatalf("expected no-backups message in output, got: %q", output)
	}
}

func TestRunReturnsErrorWhenBackupDirNotFound(t *testing.T) {
	t.Parallel()
	cfg := &util.Config{BackupDirectory: filepath.Join(t.TempDir(), "does-not-exist")}
	if err := Run(context.Background(), &ui.Console{}, cfg, ""); err == nil || !strings.Contains(err.Error(), "Failed to scan backup directory") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func pipeStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	if _, err := fmt.Fprint(w, input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})
}

func TestRunCancelsSelectionWhenUserEntersQ(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("cancel-pw"))
	pipeStdin(t, "q\n")

	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = Run(context.Background(), &ui.Console{}, &util.Config{BackupDirectory: fx.BackupDir}, "")
	})
	if runErr != nil || !strings.Contains(output, "Restore cancelled.") {
		t.Fatalf("expected cancel, got err=%v output=%q", runErr, output)
	}
}

func TestRunReturnsErrorWhenDestinationPromptClosed(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("prompt-pw"))
	// "." selects the newest backup; EOF then ends the destination prompt.
	pipeStdin(t, ".\n")

	var runErr error
	testutil.CaptureStdout(t, func() {
		runErr = Run(context.Background(), &ui.Console{}, &util.Config{BackupDirectory: fx.BackupDir}, "")
	})
	if runErr == nil {
		t.Fatal("expected error when stdin closes before destination prompt, got nil")
	}
}
