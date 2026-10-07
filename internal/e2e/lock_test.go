package e2e

import (
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// A restore or verification fails before its first question while another
// RestoreSafe process backs up into the same directory, whose retention
// could delete the parts it reads (refactoring 2.0 RF-50); a backup fails
// while a restore runs.
func TestRestoreAndVerifyWaitForARunningBackup(t *testing.T) {
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
	s = useScript(t, nil)
	if err := backup.Run(context.Background(), s.ui, cfg, ""); err == nil || !strings.Contains(err.Error(), "Another RestoreSafe window") {
		t.Fatalf("backup: expected the running restore, got %v", err)
	}
	s.done()
	// A verification runs next to a restore.
	s = useScript(t, []string{"y"}, password)
	if err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: sets}); err != nil {
		t.Fatalf("verify next to a restore: %v", err)
	}
	s.done()
}
