package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/testutil/inventory"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/job"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"
)

// partPaths returns the part files of the fixture's set, in order.
func partPaths(t *testing.T, fx *testutil.BackupFixture) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fx.BackupDir, "*.enc"))
	if err != nil || len(paths) < 2 {
		t.Fatalf("expected several part files, got %v (%v)", paths, err)
	}
	return paths
}

func TestRunVerifiesAfterUnlockingWithThePassword(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("verify-run-pw"))

	var out testutil.Output
	s := interacttest.Answering(&out, "y", fx.Password)
	if err := Run(context.Background(), s, fx.Config(), "", Request{Sets: []naming.BackupEntry{fx.Entry}}); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if s.LogPath == "" || !strings.Contains(out.String(), "Log file: "+s.LogPath) {
		t.Fatalf("expected the result with the log file, got %q", out.String())
	}
	facts, err := logging.ReadFacts(s.LogPath)
	if fact := facts.Verify[fx.Entry.String()]; err != nil || len(facts.Verify) != 1 || fact.Result != logging.ResultOK {
		t.Fatalf("expected one successful verify fact, got %+v (%v)", facts.Verify, err)
	}
}

func TestRunStopsWhenTheUserDeclinesTheStart(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))

	var out testutil.Output
	s := interacttest.Answering(&out, "n", nil) // no password: unlocking would fail
	if err := Run(context.Background(), s, fx.Config(), "", Request{Sets: []naming.BackupEntry{fx.Entry}}); err != nil {
		t.Fatalf("a declined start is no error, got %v", err)
	}
	if !strings.Contains(out.String(), "Verification cancelled.") {
		t.Fatalf("expected the cancellation to be reported, got %q", out.String())
	}
}

func TestRunRefusesADifferentialWithoutItsFullBackupBeforeAsking(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	full, err := filepath.Glob(filepath.Join(fx.BackupDir, "*_FULL-*.enc"))
	if err != nil || len(full) == 0 {
		t.Fatalf("expected the full backup's parts, got %v (%v)", full, err)
	}
	for _, p := range full {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}

	var out testutil.Output
	s := &interacttest.Script{Out: &out} // any question would fail with "no scripted answer"
	err = Run(context.Background(), s, fx.Config(), "", Request{Sets: []naming.BackupEntry{diff}})
	if err == nil || !strings.Contains(err.Error(), "Verification preflight failed") {
		t.Fatalf("expected the preflight to refuse the differential, got %v\n%s", err, out.String())
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
	// at a time: a lock left from an earlier run, which the verification waits for
	// until Cancel (core spec 14, refactoring plan RF-126). Another
	// RestoreSafe's backup is refused at once (fsx tests).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &interacttest.Script{OnLockWait: cancel}
	err = Run(ctx, u, fx.Config(), "", Request{Sets: []naming.BackupEntry{fx.Entry}})
	if !errors.Is(err, context.Canceled) || len(u.LockWaits) != 1 {
		t.Fatalf("expected the verification to wait for the lock until Cancel, got %v, waits %q", err, u.LockWaits)
	}
}

func TestVerifyAllReportsADamagedPartAndLogsTheFailure(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	parts := partPaths(t, fx)
	data, err := os.ReadFile(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(parts[1], data, 0o600); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(t.TempDir(), "verify.log")
	log, err := logging.NewLogger(logPath, "info", nil)
	if err != nil {
		t.Fatal(err)
	}
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, logPath: logPath, masters: unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log: log}
	runErr := op.run(context.Background(), &interacttest.Script{}, 0)
	log.Close()
	if runErr == nil || !strings.Contains(runErr.Error(), "Failed to verify directory") {
		t.Fatalf("expected the damaged part to fail the verification, got %v", runErr)
	}
	facts, err := logging.ReadFacts(logPath)
	if fact := facts.Verify[fx.Entry.String()]; err != nil || len(facts.Verify) != 1 || fact.Result != logging.ResultFailed || fact.Error == "" {
		t.Fatalf("expected one failed verify fact with its reason, got %+v (%v)", facts.Verify, err)
	}
	if lines := testutil.LogErrors(t, logPath); len(lines) != 1 || !strings.HasPrefix(lines[0], "Failed to verify directory") {
		t.Fatalf("the failure must be an ERROR line with its reason, got %q", lines)
	}
}

func TestRunReportsACancelledVerificationWithoutAFailureFact(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))

	logPath := filepath.Join(t.TempDir(), "verify.log")
	log, err := logging.NewLogger(logPath, "info", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, logPath: logPath, masters: unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log: log}
	runErr := op.run(ctx, &interacttest.Script{}, 0)
	log.Close()
	if !errors.Is(runErr, context.Canceled) || runErr.Error() != job.Cancelled("Verification").Error() {
		t.Fatalf("expected the verification to be cancelled, got %v", runErr)
	}
	if facts, err := logging.ReadFacts(logPath); err != nil || len(facts.Verify) != 0 {
		t.Fatalf("a cancelled verification writes no fact, got %+v (%v)", facts.Verify, err)
	}
	if lines := testutil.LogErrors(t, logPath); len(lines) != 0 {
		t.Fatalf("a cancellation is no error, got %q", lines)
	}
}

func TestVerifyDifferentialRestorePoint(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "nested", "small.txt"), []byte("changed content"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := inventory.Read(t, fx.BackupDir)
	selected, err := job.SelectSets(infos, []naming.BackupEntry{diff})
	if err != nil {
		t.Fatal(err)
	}

	items := job.SelectionPreflight(selected, infos)
	if items[0].Err != nil || items[0].Base == nil || items[0].Base.Entry != fx.Entry {
		t.Fatalf("differential must find its full backup: %+v", items[0])
	}
	var out testutil.Output
	op := &operation{selected: selected, inventory: infos, backupDir: fx.BackupDir, masters: unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log: logging.NewOutputLogger("info", &out)}
	_, err = op.verifyAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("verify differential: %v", err)
	}
	if output := out.String(); !strings.Contains(output, "2 file(s), 1 directory(s)") || !strings.Contains(output, "Reading unchanged files from the full backup") {
		t.Fatalf("expected the complete restore point to be verified: %q", output)
	}
}

func TestOperationRunVerifiesFixture(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-pw"))
	logPath := filepath.Join(t.TempDir(), "verify.log")
	var out testutil.Output
	log, _ := logging.NewLogger(logPath, "info", &out)

	infos := inventory.Read(t, fx.BackupDir)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, logPath: logPath, masters: unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log: log}
	err := op.run(context.Background(), &interacttest.Script{Out: &out}, 0)
	log.Close()
	output := out.String()
	if err != nil {
		t.Fatalf("operation.run: %v", err)
	}
	if !strings.Contains(output, "2 file(s), 1 directory(s)") || !strings.Contains(output, "Verification completed successfully.") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestVerifyEntryRejectsWrongKey(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("right"))
	wrong, _ := cryptox.RandomBytes(cryptox.KeyLen)
	_, err := (&operation{backupDir: fx.BackupDir, log: logging.NewOutputLogger("info", nil)}).verifyEntry(context.Background(), nil, fx.Entry, nil, wrong)
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}

func TestVerifyAllVerifiesSeveralSets(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	fx.CreateBackupInDir(t, naming.BackupEntry{DirectoryName: "Second", ChainID: "SEC001", Date: "2026-03-15"})
	infos := inventory.Read(t, fx.BackupDir)
	if len(infos) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(infos))
	}
	var out testutil.Output
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, masters: unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log: logging.NewOutputLogger("info", &out)}
	_, err := op.verifyAll(context.Background(), nil)
	output := out.String()
	if err != nil || strings.Count(output, "successfully verified") != 2 {
		t.Fatalf("expected both sets verified, err=%v output=%q", err, output)
	}
}

func TestVerifyEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	entry := naming.BackupEntry{DirectoryName: "Ghost", ChainID: "GHO001", Date: "2026-03-14"}
	_, err := (&operation{backupDir: t.TempDir(), log: logging.NewOutputLogger("info", nil)}).verifyEntry(context.Background(), nil, entry, nil, make([]byte, 32))
	if err == nil || !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got %v", err)
	}
}

func TestRunReturnsErrorWhenBackupDirNotFound(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{BackupDirectory: filepath.Join(t.TempDir(), "does-not-exist")}
	req := Request{Sets: []naming.BackupEntry{{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}}}
	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", req); err == nil || !strings.Contains(err.Error(), "Failed to scan backup directory") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func TestRunRejectsEmptyAndUnknownRequests(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	cfg := &config.Config{BackupDirectory: fx.BackupDir}

	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", Request{}); err == nil || !strings.Contains(err.Error(), "No backup chosen") {
		t.Fatalf("empty request: got %v", err)
	}
	unknown := Request{Sets: []naming.BackupEntry{{DirectoryName: "Other", ChainID: "ZZZ999", Date: "2026-03-14"}}}
	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", unknown); err == nil || !strings.Contains(err.Error(), "no longer in the backup directory") {
		t.Fatalf("unknown backup: got %v", err)
	}
}

func TestRunReturnsErrorWhenStartIsNotAnswered(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))

	var out strings.Builder
	runErr := Run(context.Background(), &interacttest.Script{Out: &out}, &config.Config{BackupDirectory: fx.BackupDir, LogLevel: "info"}, "", Request{Sets: []naming.BackupEntry{fx.Entry}})
	if runErr == nil {
		t.Fatal("expected an error when the start prompt gets no answer, got nil")
	}
}
