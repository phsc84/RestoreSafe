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
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/job"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"
)

// script answers the start question with answer and every password prompt
// with password.
func script(out *testutil.Output, answer string, password []byte) *interacttest.Script {
	return &interacttest.Script{
		Out:          out,
		ReadLine:     func(string) (string, error) { return answer, nil },
		ReadPassword: func(string) ([]byte, error) { return append([]byte(nil), password...), nil },
	}
}

func fixtureConfig(fx *testutil.BackupFixture) *config.Config {
	return &config.Config{BackupDirectory: fx.BackupDir, LogLevel: "info"}
}

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
	s := script(&out, "y", fx.Password)
	if err := Run(context.Background(), s, fixtureConfig(fx), "", Request{Sets: []naming.BackupEntry{fx.Entry}}); err != nil {
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
	s := script(&out, "n", nil) // no password: unlocking would fail
	if err := Run(context.Background(), s, fixtureConfig(fx), "", Request{Sets: []naming.BackupEntry{fx.Entry}}); err != nil {
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
	err = Run(context.Background(), s, fixtureConfig(fx), "", Request{Sets: []naming.BackupEntry{diff}})
	if err == nil || !strings.Contains(err.Error(), "Verification preflight failed") {
		t.Fatalf("expected the preflight to refuse the differential, got %v\n%s", err, out.String())
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

	err = Run(context.Background(), &interacttest.Script{}, fixtureConfig(fx), "", Request{Sets: []naming.BackupEntry{fx.Entry}})
	if err == nil || !strings.Contains(err.Error(), "A backup is running") {
		t.Fatalf("expected the running backup to block the verification, got %v", err)
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
	infos := fixtureInfos(t, fx)
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
	infos := fixtureInfos(t, fx)
	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, logPath: logPath, masters: unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log: log}
	runErr := op.run(ctx, &interacttest.Script{}, 0)
	log.Close()
	if !errors.Is(runErr, context.Canceled) || runErr.Error() != job.Cancelled("Verification").Error() {
		t.Fatalf("expected the verification to be cancelled, got %v", runErr)
	}
	if facts, err := logging.ReadFacts(logPath); err != nil || len(facts.Verify) != 0 {
		t.Fatalf("a cancelled verification writes no fact, got %+v (%v)", facts.Verify, err)
	}
}
