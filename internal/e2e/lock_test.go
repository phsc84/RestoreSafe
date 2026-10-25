package e2e

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/workflow/backup"
	"github.com/phsc84/restoresafe/internal/workflow/restore"
	"github.com/phsc84/restoresafe/internal/workflow/verify"
)

// A restore or verification never runs beside a backup of the same
// directory, whose retention could delete the parts it reads (refactoring
// 2.0 RF-50), and a backup never beside a restore. The locks are held by
// this process here, which runs one operation at a time, so every run takes
// the lock for one left after a lost connection and waits for it before its
// first question, until Cancel (core spec 14, refactoring plan RF-126); the
// refusals of a lock that another RestoreSafe holds are tested in fsx.
func TestRunsWaitForTheLockOfAnotherRun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	docs := filepath.Join(root, "Docs")
	writeFile(t, filepath.Join(docs, "a.txt"), "content")
	cfg := progressConfig([]string{docs}, filepath.Join(root, "Backups"))
	runBackup(t, cfg, yes, password, password)
	sets := newestRun(t, cfg)

	backupLock, err := fsx.AcquireBackupLock(cfg.BackupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		name string
		run  func(context.Context, *script) error
	}{
		{"restore", func(ctx context.Context, s *script) error {
			return restore.Run(ctx, s.ui, cfg, "", restore.Request{Sets: sets, Destination: filepath.Join(root, "Restore")})
		}},
		{"verify", func(ctx context.Context, s *script) error {
			return verify.Run(ctx, s.ui, cfg, "", verify.Request{Sets: sets})
		}},
	} {
		s := useScript(t, nil) // no question may be asked
		ctx, cancel := context.WithCancel(context.Background())
		s.ui.OnLockWait = cancel
		if err := run.run(ctx, s); !errors.Is(err, context.Canceled) || len(s.ui.LockWaits) != 1 {
			t.Fatalf("%s: expected the wait for the backup's lock until Cancel, got %v, waits %q", run.name, err, s.ui.LockWaits)
		}
		cancel()
		s.done()
	}
	backupLock.Release()

	readLock, err := fsx.AcquireReadLock(cfg.BackupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer readLock.Release()
	// A shared lock has no holder line, and no other RestoreSafe process runs:
	// the backup takes it for a lock left after a lost connection and waits
	// for it, until Cancel (core spec 14, refactoring plan RF-126).
	s := useScript(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ui.OnLockWait = cancel
	if err := backup.Run(ctx, s.ui, cfg, ""); !errors.Is(err, context.Canceled) || len(s.ui.LockWaits) != 1 {
		t.Fatalf("backup: expected the wait for the lock until Cancel, got %v, waits %q", err, s.ui.LockWaits)
	}
	s.done()
	// A verification runs next to a restore.
	s = useScript(t, yes, password)
	if err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: sets}); err != nil {
		t.Fatalf("verify next to a restore: %v", err)
	}
	s.done()
}
