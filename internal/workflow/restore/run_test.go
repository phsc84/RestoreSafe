package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bytes"
	"fmt"
	"io/fs"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/format/setwriter"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/testutil/filelock"
	"github.com/phsc84/restoresafe/internal/testutil/inventory"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/job"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
)

// request restores the fixture's set into a new destination.
func request(t *testing.T, fx *testutil.BackupFixture) Request {
	return Request{Sets: []naming.BackupEntry{fx.Entry}, Destination: filepath.Join(t.TempDir(), "restore")}
}

// damagePart flips a byte in the middle of the set's second part file.
func damagePart(t *testing.T, fx *testutil.BackupFixture) {
	t.Helper()
	parts, err := filepath.Glob(filepath.Join(fx.BackupDir, "*.enc"))
	if err != nil || len(parts) < 2 {
		t.Fatalf("expected several part files, got %v (%v)", parts, err)
	}
	data, err := os.ReadFile(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(parts[1], data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunRestoresAfterUnlockingWithThePassword(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("restore-run-pw"))
	req := request(t, fx)

	s := interacttest.Starting(string(fx.Password))
	if err := Run(context.Background(), s, fx.Config(), "", req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(req.Destination, fx.Entry.DirectoryName))
	if s.LogPath == "" || len(s.Results) != 1 || s.Results[0].LogPath != s.LogPath {
		t.Fatalf("expected the result with the log file %q, got %+v", s.LogPath, s.Results)
	}
}

func TestRunWritesNothingWhenTheUserDeclinesTheStart(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	req := request(t, fx)

	s := &interacttest.Script{Starts: []interact.BackupStart{interact.BackupCancel}}
	if err := Run(context.Background(), s, fx.Config(), "", req); err != nil {
		t.Fatalf("a declined start is no error, got %v", err)
	}
	if log, err := os.ReadFile(s.LogPath); err != nil || !strings.Contains(string(log), "Restore cancelled by user before start") {
		t.Fatalf("expected the cancellation in the log, got %q (%v)", log, err)
	}
	if _, err := os.Stat(req.Destination); !os.IsNotExist(err) {
		t.Fatalf("a declined restore writes nothing, but %s exists", req.Destination)
	}
}

func TestRunRefusesADestinationThatHoldsTheFolderBeforeAsking(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	req := request(t, fx)
	existing := filepath.Join(req.Destination, fx.Entry.DirectoryName)
	if err := os.MkdirAll(existing, 0o750); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), &interacttest.Script{}, fx.Config(), "", req)
	if err == nil || !strings.Contains(err.Error(), "Restore preflight failed") {
		t.Fatalf("expected the preflight to refuse the destination, got %v", err)
	}
	if entries, _ := os.ReadDir(existing); len(entries) != 0 {
		t.Fatalf("the existing folder must stay untouched, got %d entries", len(entries))
	}
}

func TestRunWaitsForALockLeftByThisProcess(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	lock, err := fsx.AcquireBackupLock(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	// The lock's holder line names this process, which runs one operation
	// at a time: a lock left from an earlier run, which the restore waits for
	// until Cancel (core spec 14, refactoring plan RF-126). Another
	// RestoreSafe's backup is refused at once (fsx tests).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &interacttest.Script{OnLockWait: cancel}
	err = Run(ctx, u, fx.Config(), "", request(t, fx))
	if !errors.Is(err, context.Canceled) || len(u.LockWaits) != 1 {
		t.Fatalf("expected the restore to wait for the lock until Cancel, got %v, waits %q", err, u.LockWaits)
	}
}

func TestRunReportsADamagedPartAndTheIncompleteFolder(t *testing.T) {
	t.Parallel()
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	damagePart(t, fx.BackupFixture)

	log, out := testutil.NewLog(t, "info")
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: log}
	err := op.run(context.Background(), &interacttest.Script{}, 0)
	if err == nil || !strings.Contains(err.Error(), "Failed to restore directory") || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected the damaged part to fail the restore, got %v", err)
	}
	if !strings.Contains(out(), "is INCOMPLETE") {
		t.Fatalf("expected the incomplete folder to be named, got %q", out())
	}
	lines := testutil.ErrorLines(out())
	if len(lines) != 2 || !strings.Contains(lines[0], "is INCOMPLETE") || !strings.HasPrefix(lines[1], "Failed to restore directory") {
		t.Fatalf("the incomplete folder and the failure must be ERROR lines, got %q", lines)
	}
}

func TestRunWarnsAboutFilesTheBackupCouldNotRead(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "Mail")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"readable.txt": "kept", "archive.pst": "held open by Outlook"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatal(err)
	}
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	entry := naming.BackupEntry{DirectoryName: "Mail", ChainID: "SKP001", Date: "2026-03-14"}
	release := filelock.Hold(t, filepath.Join(src, "archive.pst"))
	_, err := setwriter.Write(context.Background(), setwriter.Params{
		SourceDir: src, OutputDir: backupDir, Entry: entry, RunID: entry.ChainID,
		KeySet: *ks, Master: master, SplitSizeBytes: 1024 * 1024, SkipUnreadable: true,
	})
	release()
	if err != nil {
		t.Fatalf("backup with an unreadable file: %v", err)
	}

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "2026-03-14_SKP001.log")
	log, err := logging.NewLogger(logPath, "info")
	if err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(t.TempDir(), "restore")
	op := &operation{selected: infos, inventory: infos, backupDir: backupDir, restorePath: restorePath, logPath: logPath, masters: unlock.MasterKeys{ks.ID: master}, log: log}
	ui := &interacttest.Script{}
	runErr := op.run(context.Background(), ui, 0)
	log.Close()
	if runErr != nil {
		t.Fatalf("restore: %v", runErr)
	}
	if len(ui.Results) != 1 || ui.Results[0].Warnings != 1 {
		t.Fatalf("expected the result to count a warning, got %+v", ui.Results)
	}
	facts, err := logging.ReadFacts(logPath)
	if fact := facts.Restored[entry.String()]; err != nil || fact.Result != logging.ResultWarnings || fact.Skipped != 1 {
		t.Fatalf("expected a restore fact with one skipped file, got %+v (%v)", facts.Restored, err)
	}
	if _, err := os.Stat(filepath.Join(restorePath, "Mail", "readable.txt")); err != nil {
		t.Fatalf("the readable file must be restored: %v", err)
	}
}

func TestRunCancelledBeforeTheFirstSetLeavesNoFolder(t *testing.T) {
	t.Parallel()
	fx := testutil.NewRestoreFixture(t, []byte("pw"))

	log, out := testutil.NewLog(t, "info")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: log}
	err := op.run(ctx, &interacttest.Script{}, 0)
	if !errors.Is(err, context.Canceled) || err.Error() != job.Cancelled("Restore").Error() {
		t.Fatalf("expected the restore to be cancelled, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName)); !os.IsNotExist(statErr) {
		t.Fatal("a restore cancelled before its first byte must not leave an empty folder")
	}
	if !strings.Contains(out(), "Restore cancelled.") {
		t.Fatalf("expected the cancellation in the log, got %q", out())
	}
	if lines := testutil.ErrorLines(out()); len(lines) != 0 {
		t.Fatalf("a cancellation is no error, got %q", lines)
	}
}

// cancelOnRestore cancels the operation at its first report of restoring a
// folder: after the folder is created, before its content is written.
type cancelOnRestore struct {
	*interacttest.Script
	cancel context.CancelFunc
}

func (c cancelOnRestore) Progress(p interact.Progress) {
	if p.Phase == interact.PhaseRestoring {
		c.cancel()
	}
}

func TestRunCancelledWhileWritingMarksTheFolderIncompleteAsAWarning(t *testing.T) {
	t.Parallel()
	fx := testutil.NewRestoreFixture(t, []byte("pw"))

	log, out := testutil.NewLog(t, "info")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: log}
	err := op.run(ctx, cancelOnRestore{Script: &interacttest.Script{}, cancel: cancel}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the restore to be cancelled, got %v", err)
	}
	if !strings.Contains(out(), "] "+logging.SeverityWarn+" -   The restore of ["+fx.Entry.DirectoryName+"] is INCOMPLETE") {
		t.Fatalf("a cancelled folder is INCOMPLETE as a warning, got %q", out())
	}
	if lines := testutil.ErrorLines(out()); len(lines) != 0 {
		t.Fatalf("a cancellation is no error, got %q", lines)
	}
}

func masterKeys(fx *testutil.BackupFixture) unlock.MasterKeys {
	return unlock.MasterKeys{fx.KeySet.ID: append([]byte(nil), fx.Master...)}
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

func TestOperationRunRestoresFixture(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("restore-pw"))
	infos := inventory.Read(t, fx.BackupDir)
	logPath := filepath.Join(t.TempDir(), "restore.log")
	log, _ := logging.NewLogger(logPath, "info")

	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, logPath: logPath, masters: masterKeys(fx.BackupFixture), log: log}
	err := op.run(context.Background(), &interacttest.Script{}, 0)
	log.Close()
	data, _ := os.ReadFile(logPath)
	output := string(data)
	if err != nil {
		t.Fatalf("operation.run: %v", err)
	}
	if !strings.Contains(output, "Restore completed successfully.") || !strings.Contains(output, "successfully restored and checked") {
		t.Fatalf("unexpected output: %q", output)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
}

// TestOperationRunCountsALogLeftInTemp: a restore whose log can't be added
// to the backup directory (its path is a directory) still restores, reports
// the copy in TEMP and counts it as a warning (RF-109).
func TestOperationRunCountsALogLeftInTemp(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("restore-pw"))
	infos := inventory.Read(t, fx.BackupDir)
	logPath := filepath.Join(t.TempDir(), "restore.log")
	if err := os.Mkdir(logPath, 0o750); err != nil {
		t.Fatal(err)
	}
	log, _ := logging.NewLogger(logPath, "info")
	ui := &interacttest.Script{}

	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, logPath: logPath, masters: masterKeys(fx.BackupFixture), log: log}
	if err := op.run(context.Background(), ui, 0); err != nil {
		t.Fatalf("operation.run: %v", err)
	}
	if len(ui.LogFailures) != 1 || ui.LogFailures[0].Kind != interact.LogKept {
		t.Fatalf("log failures %+v, want the copy in TEMP", ui.LogFailures)
	}
	t.Cleanup(func() { os.Remove(ui.LogFailures[0].Kept) })
	if len(ui.Results) != 1 || ui.Results[0].Warnings != 1 {
		t.Fatalf("the result doesn't count the problem: %+v", ui.Results)
	}
}

func TestRestoreAllRestoresDifferential(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "nested", "small.txt"), []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := inventory.Read(t, fx.BackupDir)
	selected, err := job.SelectSets(infos, []naming.BackupEntry{diff})
	if err != nil {
		t.Fatal(err)
	}

	op := &operation{selected: selected, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture)}
	_, err = op.restoreAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("restore differential: %v", err)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
}

func TestRestoreEntryRejectsWrongKey(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("right"))
	wrong, _ := cryptox.RandomBytes(cryptox.KeyLen)

	_, err := entryOperation(fx.BackupDir, fx.RestoreRoot).restoreEntry(context.Background(), nil, fx.Entry, nil, wrong)
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}

func TestRestoreEntryRefusesExistingDestination(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	if err := os.Mkdir(filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := entryOperation(fx.BackupDir, fx.RestoreRoot).restoreEntry(context.Background(), nil, fx.Entry, nil, fx.Master)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-destination error, got %v", err)
	}
}

func TestRestoreEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}
	_, err := entryOperation(t.TempDir(), t.TempDir()).restoreEntry(context.Background(), nil, entry, nil, make([]byte, 32))
	if err == nil || !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got %v", err)
	}
}

func TestRunReturnsErrorWhenBackupDirNotFound(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{BackupDirectory: filepath.Join(t.TempDir(), "does-not-exist")}
	req := Request{Sets: []naming.BackupEntry{{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}}, Destination: t.TempDir()}
	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", req); err == nil || !strings.Contains(err.Error(), "Failed to scan backup directory") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func TestRunRejectsIncompleteRequests(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	cfg := &config.Config{BackupDirectory: fx.BackupDir}
	dest := filepath.Join(t.TempDir(), "restore")

	for _, tc := range []struct {
		name string
		req  Request
		want string
	}{
		{"no destination", Request{Sets: []naming.BackupEntry{fx.Entry}}, "No restore destination chosen"},
		{"no backup", Request{Destination: dest}, "No backup chosen"},
		{"unknown backup", Request{Sets: []naming.BackupEntry{{DirectoryName: "Other", ChainID: "ZZZ999", Date: "2026-03-14"}}, Destination: dest}, "no longer in the backup directory"},
	} {
		err := Run(context.Background(), &interacttest.Script{}, cfg, "", tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error with %q, got %v", tc.name, tc.want, err)
		}
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("nothing may be written for a rejected request")
	}
}

func TestRunReturnsErrorWhenStartIsNotAnswered(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("prompt-pw"))

	req := Request{Sets: []naming.BackupEntry{fx.Entry}, Destination: filepath.Join(t.TempDir(), "restore")}
	runErr := Run(context.Background(), &interacttest.Script{}, &config.Config{BackupDirectory: fx.BackupDir}, "", req)
	if runErr == nil {
		t.Fatal("expected an error when the start prompt gets no answer, got nil")
	}
}

// entryOperation is a restore from backupDir into restorePath that logs to
// nowhere, for tests of single sets.
func entryOperation(backupDir, restorePath string) *operation {
	return &operation{backupDir: backupDir, restorePath: restorePath}
}
