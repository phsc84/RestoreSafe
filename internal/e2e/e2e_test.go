// Package e2e drives the interactive backup, verify, and restore workflows end
// to end with scripted answers.
package e2e

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const password = "correct horse battery"

var disabled = false

// fullBackupsOnly disables differentials for tests about full backups.
var fullBackupsOnly = config.Differential{Enabled: &disabled}

// script feeds scripted answers to the workflows. Running out of answers fails
// the test, so every prompt of a workflow is accounted for.
type script struct {
	t         *testing.T
	lines     []string
	passwords []string
	ui        *interacttest.Script
}

func useScript(t *testing.T, lines []string, passwords ...string) *script {
	t.Helper()
	s := &script{t: t, lines: lines, passwords: passwords}
	s.ui = &interacttest.Script{
		ReadLine: func(prompt string) (string, error) {
			if len(s.lines) == 0 {
				return "", fmt.Errorf("unexpected line prompt %q", prompt)
			}
			line := s.lines[0]
			s.lines = s.lines[1:]
			return line, nil
		},
		ReadPassword: func(prompt string) ([]byte, error) {
			if len(s.passwords) == 0 {
				return nil, fmt.Errorf("unexpected password prompt %q", prompt)
			}
			pw := s.passwords[0]
			s.passwords = s.passwords[1:]
			return []byte(pw), nil
		},
	}
	return s
}

func (s *script) done() {
	s.t.Helper()
	if len(s.lines) != 0 || len(s.passwords) != 0 {
		s.t.Fatalf("not all scripted input was used: lines=%v passwords=%d", s.lines, len(s.passwords))
	}
}

// newestRun returns the backup sets of the newest backup run, as the user
// chooses them for a restore or verify.
func newestRun(t *testing.T, cfg *config.Config) []naming.BackupEntry {
	t.Helper()
	runs := backupRuns(t, cfg)
	if len(runs) == 0 {
		t.Fatal("no backup run to choose")
	}
	return runs[0].Entries
}

// runSets returns the backup sets written by the backup run runID.
func runSets(t *testing.T, cfg *config.Config, runID string) []naming.BackupEntry {
	t.Helper()
	for _, run := range backupRuns(t, cfg) {
		if string(run.RunID) == runID {
			return run.Entries
		}
	}
	t.Fatalf("no backup run %s", runID)
	return nil
}

func backupRuns(t *testing.T, cfg *config.Config) []catalog.BackupRunSummary {
	t.Helper()
	infos, err := catalog.Inventory(cfg.BackupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	return catalog.BackupRunSummaries(infos)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertTreesEqual compares structure, content, and modification times.
func assertTreesEqual(t *testing.T, want, got string) {
	t.Helper()
	count := 0
	err := filepath.WalkDir(want, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(want, path)
		other := filepath.Join(got, rel)
		wi, _ := d.Info()
		gi, err := os.Stat(other)
		if err != nil {
			return fmt.Errorf("%s missing in restore", rel)
		}
		if wi.IsDir() != gi.IsDir() {
			return fmt.Errorf("%s: type differs", rel)
		}
		if !wi.IsDir() {
			a, _ := os.ReadFile(path)
			b, _ := os.ReadFile(other)
			if !bytes.Equal(a, b) {
				return fmt.Errorf("%s: content differs", rel)
			}
			if !wi.ModTime().Equal(gi.ModTime()) {
				return fmt.Errorf("%s: modification time %v, want %v", rel, gi.ModTime(), wi.ModTime())
			}
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing extra in the restore.
	extra := 0
	filepath.WalkDir(got, func(string, fs.DirEntry, error) error { extra++; return nil })
	if extra != count {
		t.Fatalf("restore has %d entries, source has %d", extra, count)
	}
}

func TestBackupVerifyRestoreEndToEnd(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	backupDir := filepath.Join(root, "Backups")
	writeFile(t, filepath.Join(docs, "letter.txt"), "Dear RestoreSafe")
	writeFile(t, filepath.Join(docs, "photos", "big.bin"), strings.Repeat("x", 3*1024*1024))
	if err := os.MkdirAll(filepath.Join(docs, "empty"), 0o750); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2019, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(docs, "letter.txt"), old, old); err != nil {
		t.Fatal(err)
	}

	// A 1.x backup and a leftover of an interrupted backup in the directory.
	legacy := filepath.Join(backupDir, "[Documents]_2025-01-01_OLD001-001.enc")
	writeFile(t, legacy, "1.x data")
	leftover := filepath.Join(backupDir, "[Documents]_ZZZ999_2025-01-01_FULL-001.enc.tmp")
	writeFile(t, leftover, "partial")

	cfg := &config.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		RetentionKeep:      2,
		LogLevel:           "info",
		VerifyAfterBackup:  true,
		AuthenticationMode: config.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
	}

	// Run 1: new keys (password entered twice), full backup.
	s := useScript(t, []string{"y"}, password, password)
	out := testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup 1: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "new keys will be created") || !strings.Contains(out, "Post-backup verification successful") {
		t.Fatalf("unexpected backup 1 output: %q", out)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("leftover of interrupted backup was not removed")
	}

	infos, _ := catalog.Inventory(backupDir)
	if len(infos) != 1 || !infos[0].Complete() {
		t.Fatalf("expected one complete set after run 1, got %+v", infos)
	}
	firstKeySet := infos[0].Header.KeySet.ID

	// Runs 2 and 3: existing keys (password entered once), source changes,
	// differential backups.
	for run := 2; run <= 3; run++ {
		writeFile(t, filepath.Join(docs, fmt.Sprintf("new-%d.txt", run)), "added in run "+fmt.Sprint(run))
		writeFile(t, filepath.Join(docs, "letter.txt"), fmt.Sprintf("Dear RestoreSafe, version %d", run))
		s := useScript(t, []string{"y"}, password)
		out := testutil.CaptureStdout(t, func() {
			if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
				t.Fatalf("backup %d: %v", run, err)
			}
		})
		s.done()
		if !strings.Contains(out, "existing keys") || !strings.Contains(out, fmt.Sprintf("DIFF%03d", run-1)) {
			t.Fatalf("run %d must reuse keys and write a differential: %q", run, out)
		}
	}

	infos, _ = catalog.Inventory(backupDir)
	if len(infos) != 3 {
		t.Fatalf("expected full + 2 differentials in one chain, got %d sets", len(infos))
	}
	for _, info := range infos {
		if !info.Complete() || info.Header.KeySet.ID != firstKeySet {
			t.Fatalf("expected complete sets sharing the first key set: %+v", info)
		}
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("1.x backup file must never be touched")
	}
	logs, _ := filepath.Glob(filepath.Join(backupDir, "*.log"))
	if len(logs) != 3 {
		t.Fatalf("expected the 3 logs of the kept runs, got %v", logs)
	}

	// Verify the newest backup.
	s = useScript(t, []string{"y"}, password)
	out = testutil.CaptureStdout(t, func() {
		if err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: newestRun(t, cfg)}); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "Verification completed successfully.") || !strings.Contains(out, "→ with full backup") {
		t.Fatalf("expected the newest differential to be verified with its full backup: %q", out)
	}

	// A wrong password three times ends the restore without writing anything.
	dest := filepath.Join(root, "Restore")
	s = useScript(t, []string{"y"}, "wrong", "wrong", "wrong")
	var err error
	testutil.CaptureStdout(t, func() {
		err = restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: dest})
	})
	s.done()
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected wrong-password failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Documents")); !os.IsNotExist(err) {
		t.Fatal("nothing may be restored after failed authentication")
	}

	// Restore the newest backup and compare with the source.
	s = useScript(t, []string{"y"}, password)
	out = testutil.CaptureStdout(t, func() {
		if err := restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: dest}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "Restore completed successfully.") || !strings.Contains(out, "Reading unchanged files from the full backup") {
		t.Fatalf("unexpected restore output: %q", out)
	}
	assertTreesEqual(t, docs, filepath.Join(dest, "Documents"))

	// The full backup still restores the state of run 1.
	fullDest := filepath.Join(root, "RestoreFull")
	s = useScript(t, []string{"y"}, password)
	testutil.CaptureStdout(t, func() {
		if err := restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: runSets(t, cfg, string(infos[len(infos)-1].Header.RunID)), Destination: fullDest}); err != nil {
			t.Fatalf("restore full: %v", err)
		}
	})
	s.done()
	if data, _ := os.ReadFile(filepath.Join(fullDest, "Documents", "letter.txt")); string(data) != "Dear RestoreSafe" {
		t.Fatalf("full backup restored %q", data)
	}
	if _, err := os.Stat(filepath.Join(fullDest, "Documents", "new-2.txt")); !os.IsNotExist(err) {
		t.Fatal("files added after the full backup must not be in its restore")
	}
}

// TestExcludeAndUnreadableFiles backs up a source with excluded files and a
// locked file (on_unreadable_file: skip): the run completes with warnings,
// older backups are kept, and restore reports the missing file.
func TestExcludeAndUnreadableFiles(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	backupDir := filepath.Join(root, "Backups")
	writeFile(t, filepath.Join(docs, "report.docx"), "report")
	writeFile(t, filepath.Join(docs, "scratch.tmp"), "temp")
	writeFile(t, filepath.Join(docs, "Cache", "big.bin"), "cache")
	locked := filepath.Join(docs, "Mail", "archive.pst")
	writeFile(t, locked, "mail")

	matcher, err := config.NewExcludeMatcher([]string{"*.tmp", "/Cache"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		RetentionKeep:      1,
		LogLevel:           "info",
		AuthenticationMode: config.AuthModePassword,
		Differential:       fullBackupsOnly,
		Exclude:            []string{"*.tmp", "/Cache"},
		ExcludeMatcher:     matcher,
		OnUnreadableFile:   config.OnUnreadableSkip,
		Argon2:             testutil.FastArgon2Config,
	}

	// First backup: everything readable.
	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup 1: %v", err)
		}
	})
	s.done()

	// Second backup: the mail archive is locked by another program.
	p, _ := windows.UTF16PtrFromString(locked)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	s = useScript(t, []string{"y"}, password)
	out := testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup 2: %v", err)
		}
	})
	s.done()
	windows.CloseHandle(h)
	for _, want := range []string{"Skipped (could not be read): Mail/archive.pst", "Excluded by pattern: 2", "Cleanup old data skipped for [Documents]", "Backup completed with warnings", "Exclude            : *.tmp, /Cache"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %q", want, out)
		}
	}
	infos, _ := catalog.Inventory(backupDir)
	if len(infos) != 2 {
		t.Fatalf("retention must keep the older backup while files are skipped, got %d sets", len(infos))
	}
	if set := runLog(t, backupDir, infos[0]).Sets[infos[0].Entry.String()]; set.Skipped != 1 || set.Result != logging.ResultWarnings {
		t.Fatalf("the newest set must record its skipped file: %+v", set)
	}

	// Restoring the newest backup reports the skipped file; excluded files
	// were never backed up.
	dest := filepath.Join(root, "Restore")
	s = useScript(t, []string{"y"}, password)
	out = testutil.CaptureStdout(t, func() {
		if err := restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: dest}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "1 file(s) are not in this backup") || !strings.Contains(out, "Mail/archive.pst") {
		t.Fatalf("expected restore to report the skipped file: %q", out)
	}
	restored := filepath.Join(dest, "Documents")
	if data, _ := os.ReadFile(filepath.Join(restored, "report.docx")); string(data) != "report" {
		t.Fatal("report.docx not restored")
	}
	for _, missing := range []string{"scratch.tmp", "Cache", filepath.Join("Mail", "archive.pst")} {
		if _, err := os.Stat(filepath.Join(restored, missing)); !os.IsNotExist(err) {
			t.Fatalf("%s must not be restored", missing)
		}
	}
}

func runBackup(t *testing.T, cfg *config.Config, lines []string, passwords ...string) string {
	t.Helper()
	s := useScript(t, lines, passwords...)
	out := testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
	s.done()
	return out
}

// TestDifferentialChain creates a full backup and differentials on top of
// it, then starts a new chain with [F]; retention then removes the old chain
// as a whole.
func TestDifferentialChain(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	backupDir := filepath.Join(root, "Backups")
	// A large unchanged file keeps the differentials small relative to the
	// full backup (max_size_percent compares encrypted data sizes).
	writeFile(t, filepath.Join(docs, "stable.txt"), strings.Repeat("never changes ", 20000))
	writeFile(t, filepath.Join(docs, "notes.txt"), "v1")
	writeFile(t, filepath.Join(docs, "old.txt"), "will be deleted")
	cfg := &config.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		RetentionKeep:      1,
		LogLevel:           "info",
		VerifyAfterBackup:  true,
		AuthenticationMode: config.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
	}

	out := runBackup(t, cfg, []string{"y"}, password, password)
	if !strings.Contains(out, "Full backup (reason: new keys)") {
		t.Fatalf("first backup must be full: %q", out)
	}
	infos, _ := catalog.Inventory(backupDir)
	chain := infos[0].Entry.ChainID

	writeFile(t, filepath.Join(docs, "notes.txt"), "v2 with more text")
	writeFile(t, filepath.Join(docs, "new.txt"), "added")
	if err := os.Remove(filepath.Join(docs, "old.txt")); err != nil {
		t.Fatal(err)
	}
	out = runBackup(t, cfg, []string{"y"}, password)
	for _, want := range []string{"Differential backup (base: full", "DIFF001-001.enc", "2 new or changed file(s) stored", "1 unchanged file(s)", "Post-backup verification successful"} {
		if !strings.Contains(out, want) {
			t.Fatalf("differential 1: expected %q in output: %q", want, out)
		}
	}

	// Nothing changed since differential 1, but a differential holds all
	// changes since the full backup, so differential 2 stores them again.
	out = runBackup(t, cfg, []string{"y"}, password)
	if !strings.Contains(out, "DIFF002-001.enc") || !strings.Contains(out, "2 new or changed file(s) stored") {
		t.Fatalf("differential 2 must contain all changes since the full backup: %q", out)
	}

	infos, _ = catalog.Inventory(backupDir)
	if len(infos) != 3 {
		t.Fatalf("expected full + 2 differentials, got %d sets", len(infos))
	}
	for _, info := range infos {
		if !info.Complete() || info.Entry.ChainID != chain {
			t.Fatalf("all sets must be complete and in chain %s: %+v", chain, info.Entry)
		}
	}
	for _, name := range []string{"DIFF001", "DIFF002"} {
		matches, _ := filepath.Glob(filepath.Join(backupDir, "[[]Documents]_"+string(chain)+"_*_"+name+"-001.enc"))
		if len(matches) != 1 {
			t.Fatalf("expected one %s part of chain %s, got %v", name, chain, matches)
		}
	}

	// [F] starts a new chain; retention (keep 1) deletes the old chain with
	// its differentials and their logs.
	out = runBackup(t, cfg, []string{"f", "y"}, password)
	if !strings.Contains(out, "Backup type: full (full backup requested)") {
		t.Fatalf("expected forced full backup: %q", out)
	}
	infos, _ = catalog.Inventory(backupDir)
	if len(infos) != 1 || infos[0].Entry.IsDiff() || infos[0].Entry.ChainID == chain {
		t.Fatalf("expected only the new full backup to remain, got %+v", infos)
	}
	logs, _ := filepath.Glob(filepath.Join(backupDir, "*.log"))
	if len(logs) != 1 {
		t.Fatalf("expected only the log of the new chain, got %v", logs)
	}
}

// TestNewKeysKeepOldBackupsRestorable chooses [K] to change the password and
// checks that the old backup still opens with the old password only, while
// the new backup opens with the new password.
func TestNewKeysKeepOldBackupsRestorable(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	backupDir := filepath.Join(root, "Backups")
	writeFile(t, filepath.Join(docs, "a.txt"), "version 1")
	cfg := &config.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		LogLevel:           "info",
		AuthenticationMode: config.AuthModePassword,
		PasswordMinLength:  12,
		Argon2:             testutil.FastArgon2Config,
	}
	const newPassword = "a brand new password"

	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup 1: %v", err)
		}
	})
	s.done()
	infos, _ := catalog.Inventory(backupDir)
	oldRun := infos[0].Header.RunID
	oldKeys := infos[0].Header.KeySet.ID

	writeFile(t, filepath.Join(docs, "a.txt"), "version 2")
	s = useScript(t, []string{"k", "y"}, newPassword, newPassword)
	out := testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup 2: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "New keys created") {
		t.Fatalf("expected new keys, got %q", out)
	}
	infos, _ = catalog.Inventory(backupDir)
	if len(infos) != 2 || infos[0].Header.KeySet.ID == oldKeys {
		t.Fatalf("expected a second backup with new keys")
	}

	// The old backup: the new password is rejected, the old one works.
	oldDest := filepath.Join(root, "RestoreOld")
	s = useScript(t, []string{"y"}, newPassword, newPassword, newPassword)
	var err error
	testutil.CaptureStdout(t, func() {
		err = restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: runSets(t, cfg, oldRun), Destination: oldDest})
	})
	s.done()
	if err == nil {
		t.Fatal("new password must not open the old backup")
	}
	s = useScript(t, []string{"y"}, password)
	testutil.CaptureStdout(t, func() {
		if err := restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: runSets(t, cfg, oldRun), Destination: oldDest}); err != nil {
			t.Fatalf("restore old: %v", err)
		}
	})
	s.done()
	if data, _ := os.ReadFile(filepath.Join(oldDest, "Documents", "a.txt")); string(data) != "version 1" {
		t.Fatalf("old backup restored %q", data)
	}

	// The newest backup opens with the new password.
	newDest := filepath.Join(root, "RestoreNew")
	s = useScript(t, []string{"y"}, newPassword)
	testutil.CaptureStdout(t, func() {
		if err := restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: newDest}); err != nil {
			t.Fatalf("restore new: %v", err)
		}
	})
	s.done()
	if data, _ := os.ReadFile(filepath.Join(newDest, "Documents", "a.txt")); string(data) != "version 2" {
		t.Fatalf("new backup restored %q", data)
	}
}

func TestExistingKeysRejectAnotherPassword(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	writeFile(t, filepath.Join(docs, "a.txt"), "a")
	cfg := &config.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    filepath.Join(root, "Backups"),
		SplitSizeMB:        1,
		LogLevel:           "info",
		AuthenticationMode: config.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
	}

	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
	s.done()

	// The next backup reuses the existing keys, so only their password
	// unlocks them; a different password is rejected three times.
	s = useScript(t, []string{"y"}, "another password", "another password", "another password")
	var err error
	testutil.CaptureStdout(t, func() { err = backup.Run(context.Background(), s.ui, cfg, "") })
	s.done()
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected existing keys to reject another password, got %v", err)
	}
}
