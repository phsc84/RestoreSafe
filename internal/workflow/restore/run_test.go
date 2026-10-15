package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	var out testutil.Output
	s := interacttest.Answering(&out, "y", fx.Password)
	if err := Run(context.Background(), s, fx.Config(), "", req); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(req.Destination, fx.Entry.DirectoryName))
	if s.LogPath == "" || !strings.Contains(out.String(), "Log file: "+s.LogPath) {
		t.Fatalf("expected the result with the log file, got %q", out.String())
	}
}

func TestRunWritesNothingWhenTheUserDeclinesTheStart(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	req := request(t, fx)

	var out testutil.Output
	if err := Run(context.Background(), interacttest.Answering(&out, "n", nil), fx.Config(), "", req); err != nil {
		t.Fatalf("a declined start is no error, got %v", err)
	}
	if !strings.Contains(out.String(), "Restore cancelled.") {
		t.Fatalf("expected the cancellation to be reported, got %q", out.String())
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

	var out testutil.Output
	err := Run(context.Background(), &interacttest.Script{Out: &out}, fx.Config(), "", req)
	if err == nil || !strings.Contains(err.Error(), "Restore preflight failed") {
		t.Fatalf("expected the preflight to refuse the destination, got %v\n%s", err, out.String())
	}
	if entries, _ := os.ReadDir(existing); len(entries) != 0 {
		t.Fatalf("the existing folder must stay untouched, got %d entries", len(entries))
	}
}

func TestRunRefusesWhileABackupHoldsTheDirectory(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	lock, err := fsx.AcquireBackupLock(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	err = Run(context.Background(), &interacttest.Script{}, fx.Config(), "", request(t, fx))
	if err == nil || !strings.Contains(err.Error(), "A backup is running") {
		t.Fatalf("expected the running backup to block the restore, got %v", err)
	}
}

func TestRunReportsADamagedPartAndTheIncompleteFolder(t *testing.T) {
	t.Parallel()
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	damagePart(t, fx.BackupFixture)

	var out testutil.Output
	log := logging.NewOutputLogger("info", &out)
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: log}
	err := op.run(context.Background(), &interacttest.Script{Out: &out}, 0)
	if err == nil || !strings.Contains(err.Error(), "Failed to restore directory") || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected the damaged part to fail the restore, got %v", err)
	}
	if !strings.Contains(out.String(), "is INCOMPLETE") {
		t.Fatalf("expected the incomplete folder to be named, got %q", out.String())
	}
	lines := testutil.ErrorLines(out.String())
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
	log, err := logging.NewLogger(logPath, "info", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out testutil.Output
	restorePath := filepath.Join(t.TempDir(), "restore")
	op := &operation{selected: infos, inventory: infos, backupDir: backupDir, restorePath: restorePath, logPath: logPath, masters: unlock.MasterKeys{ks.ID: master}, log: log}
	runErr := op.run(context.Background(), &interacttest.Script{Out: &out}, 0)
	log.Close()
	if runErr != nil {
		t.Fatalf("restore: %v", runErr)
	}
	if !strings.Contains(out.String(), "Warnings: 1") {
		t.Fatalf("expected the result to count a warning, got %q", out.String())
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

	var out testutil.Output
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: logging.NewOutputLogger("info", &out)}
	err := op.run(ctx, &interacttest.Script{Out: &out}, 0)
	if !errors.Is(err, context.Canceled) || err.Error() != job.Cancelled("Restore").Error() {
		t.Fatalf("expected the restore to be cancelled, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName)); !os.IsNotExist(statErr) {
		t.Fatal("a restore cancelled before its first byte must not leave an empty folder")
	}
	if !strings.Contains(out.String(), "Restore cancelled.") {
		t.Fatalf("expected the cancellation in the log, got %q", out.String())
	}
	if lines := testutil.ErrorLines(out.String()); len(lines) != 0 {
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

	var out testutil.Output
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: logging.NewOutputLogger("info", &out)}
	err := op.run(ctx, cancelOnRestore{Script: &interacttest.Script{Out: &out}, cancel: cancel}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the restore to be cancelled, got %v", err)
	}
	if !strings.Contains(out.String(), "] "+logging.SeverityWarn+" -   The restore of ["+fx.Entry.DirectoryName+"] is INCOMPLETE") {
		t.Fatalf("a cancelled folder is INCOMPLETE as a warning, got %q", out.String())
	}
	if lines := testutil.ErrorLines(out.String()); len(lines) != 0 {
		t.Fatalf("a cancellation is no error, got %q", lines)
	}
}
