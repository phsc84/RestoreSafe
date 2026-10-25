package e2e

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/workflow/backup"
	"github.com/phsc84/restoresafe/internal/workflow/restore"
	"github.com/phsc84/restoresafe/internal/workflow/verify"
)

// A restore or verification fails before its first question while another
// RestoreSafe process backs up into the same directory, whose retention
// could delete the parts it reads (refactoring 2.0 RF-50); a backup waits
// while a restore holds the lock and no other RestoreSafe process runs.
func TestRestoreAndVerifyWaitForARunningBackup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	docs := filepath.Join(root, "Docs")
	writeFile(t, filepath.Join(docs, "a.txt"), "content")
	cfg := progressConfig([]string{docs}, filepath.Join(root, "Backups"))
	runBackup(t, cfg, []string{"y"}, password, password)
	sets := newestRun(t, cfg)

	backupLock, err := fsx.AcquireBackupLock(cfg.BackupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	s := useScript(t, nil) // no question may be asked
	err = restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: sets, Destination: filepath.Join(root, "Restore")})
	if err == nil || !strings.Contains(err.Error(), "A backup is running") {
		t.Fatalf("restore: expected the running backup, got %v", err)
	}
	if err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: sets}); err == nil || !strings.Contains(err.Error(), "A backup is running") {
		t.Fatalf("verify: expected the running backup, got %v", err)
	}
	s.done()
	backupLock.Release()

	readLock, err := fsx.AcquireReadLock(cfg.BackupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer readLock.Release()
	// A shared lock has no holder line, and no other RestoreSafe process runs:
	// the backup takes it for a lock left after a lost connection and waits
	// for it, until Cancel (core spec 14, refactoring plan RF-126).
	s = useScript(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ui.OnLockWait = cancel
	if err := backup.Run(ctx, s.ui, cfg, ""); !errors.Is(err, context.Canceled) || len(s.ui.LockWaits) != 1 {
		t.Fatalf("backup: expected the wait for the lock until Cancel, got %v, waits %q", err, s.ui.LockWaits)
	}
	s.done()
	// A verification runs next to a restore.
	s = useScript(t, []string{"y"}, password)
	if err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: sets}); err != nil {
		t.Fatalf("verify next to a restore: %v", err)
	}
	s.done()
}
