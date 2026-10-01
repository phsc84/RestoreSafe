package e2e

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// runLog returns the facts in the log of the run that wrote set.
func runLog(t *testing.T, backupDir string, set catalog.SetInfo) logging.RunFacts {
	t.Helper()
	path := naming.LogFileName(backupDir, set.Header.Date, naming.BackupID(set.Header.RunID))
	facts, err := logging.ReadFacts(path)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestRunFactsAreLogged(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "First")
	second := filepath.Join(root, "Second")
	writeFile(t, filepath.Join(first, "a.txt"), "first")
	writeFile(t, filepath.Join(second, "b.txt"), "second")
	backupDir := filepath.Join(root, "Backups")
	cfg := progressConfig([]string{first, second}, backupDir)

	out := runBackup(t, cfg, []string{"y"}, password, password)
	if strings.Contains(out, "FACT") {
		t.Fatalf("facts must not reach the user's output: %q", out)
	}
	infos, err := catalog.Inventory(backupDir)
	if err != nil || len(infos) != 2 {
		t.Fatalf("expected 2 sets, got %d (%v)", len(infos), err)
	}
	facts := runLog(t, backupDir, infos[0])
	if facts.Backup == nil || facts.Backup.Result != logging.ResultOK || facts.Backup.Warnings != 0 || facts.Backup.Time.IsZero() {
		t.Fatalf("expected a successful backup fact, got %+v", facts.Backup)
	}
	for _, info := range infos {
		if s, ok := facts.Sets[info.Entry.String()]; !ok || s.Result != logging.ResultOK || s.Skipped != 0 {
			t.Fatalf("the backup must record set %s: %+v", info.Entry.String(), facts.Sets)
		}
		if v, ok := facts.Verify[info.Entry.String()]; !ok || v.Result != logging.ResultOK {
			t.Fatalf("verify after backup must record %s: %+v", info.Entry.String(), facts.Verify)
		}
	}

	// A verify run adds its results to the log of the run it reads.
	s := useScript(t, []string{"y"}, password)
	testutil.CaptureStdout(t, func() {
		if err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: []naming.BackupEntry{infos[1].Entry}}); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	s.done()
	after := runLog(t, backupDir, infos[1])
	if v := after.Verify[infos[1].Entry.String()]; v.Result != logging.ResultOK || v.Time.Before(facts.Verify[infos[1].Entry.String()].Time) {
		t.Fatalf("expected the verify run's fact, got %+v", v)
	}
	if after.Backup == nil || *after.Backup != *facts.Backup {
		t.Fatalf("the backup fact must stay as it was: %+v", after.Backup)
	}
}

func TestCancelledBackupLogsItsFact(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "First")
	second := filepath.Join(root, "Second")
	writeFile(t, filepath.Join(first, "a.txt"), "first")
	writeFile(t, filepath.Join(second, "b.txt"), "second")
	backupDir := filepath.Join(root, "Backups")
	cfg := progressConfig([]string{first, second}, backupDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := useScript(t, []string{"y"}, password, password)
	o := &observedUI{Script: s.ui, onProgress: func(p interact.Progress) {
		if p.Item == "Second" {
			cancel()
		}
	}}
	testutil.CaptureStdout(t, func() { backup.Run(ctx, o, cfg, "") }) //nolint:errcheck
	s.done()

	infos, _ := catalog.Inventory(backupDir)
	if len(infos) != 1 {
		t.Fatalf("expected the set of the first directory, got %d sets", len(infos))
	}
	if facts := runLog(t, backupDir, infos[0]); facts.Backup == nil || facts.Backup.Result != logging.ResultCancelled {
		t.Fatalf("expected a cancelled backup fact, got %+v", facts.Backup)
	}
}
