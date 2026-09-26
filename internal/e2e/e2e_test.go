// Package e2e drives the interactive backup, verify, and restore workflows end
// to end with scripted console input.
package e2e

import (
	"RestoreSafe/internal/backup"
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/restore"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"RestoreSafe/internal/verify"
	"bytes"
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

// script feeds console answers to the workflows. Running out of answers fails
// the test, so every prompt of a workflow is accounted for.
type script struct {
	t         *testing.T
	lines     []string
	passwords []string
}

func useScript(t *testing.T, lines []string, passwords ...string) *script {
	t.Helper()
	s := &script{t: t, lines: lines, passwords: passwords}
	restore := security.SetInputForTest(
		func(prompt string) (string, error) {
			if len(s.lines) == 0 {
				return "", fmt.Errorf("unexpected line prompt %q", prompt)
			}
			line := s.lines[0]
			s.lines = s.lines[1:]
			return line, nil
		},
		func(prompt string) ([]byte, error) {
			if len(s.passwords) == 0 {
				return nil, fmt.Errorf("unexpected password prompt %q", prompt)
			}
			pw := s.passwords[0]
			s.passwords = s.passwords[1:]
			return []byte(pw), nil
		},
	)
	t.Cleanup(restore)
	return s
}

func (s *script) done() {
	s.t.Helper()
	if len(s.lines) != 0 || len(s.passwords) != 0 {
		s.t.Fatalf("not all scripted input was used: lines=%v passwords=%d", s.lines, len(s.passwords))
	}
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

	cfg := &util.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		RetentionKeep:      2,
		LogLevel:           "info",
		VerifyAfterBackup:  true,
		AuthenticationMode: util.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
	}

	// Run 1: new keys (password entered twice).
	s := useScript(t, []string{"y"}, password, password)
	out := testutil.CaptureStdout(t, func() {
		if err := backup.Run(cfg, ""); err != nil {
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

	// Runs 2 and 3: existing keys (password entered once), source changes.
	for run := 2; run <= 3; run++ {
		writeFile(t, filepath.Join(docs, fmt.Sprintf("new-%d.txt", run)), "added in run "+fmt.Sprint(run))
		s := useScript(t, []string{"y"}, password)
		out := testutil.CaptureStdout(t, func() {
			if err := backup.Run(cfg, ""); err != nil {
				t.Fatalf("backup %d: %v", run, err)
			}
		})
		s.done()
		if !strings.Contains(out, "existing keys") {
			t.Fatalf("run %d did not reuse keys: %q", run, out)
		}
	}

	infos, _ = catalog.Inventory(backupDir)
	if len(infos) != 2 {
		t.Fatalf("retention_keep=2 must leave 2 sets, got %d", len(infos))
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
	if len(logs) != 2 {
		t.Fatalf("expected the 2 logs of the kept runs, got %v", logs)
	}

	// Verify the newest backup.
	s = useScript(t, []string{".", "y"}, password)
	out = testutil.CaptureStdout(t, func() {
		if err := verify.Run(cfg, ""); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "Verification completed successfully.") {
		t.Fatalf("unexpected verify output: %q", out)
	}

	// A wrong password three times ends the restore without writing anything.
	dest := filepath.Join(root, "Restore")
	s = useScript(t, []string{".", dest, "y"}, "wrong", "wrong", "wrong")
	var err error
	testutil.CaptureStdout(t, func() { err = restore.Run(cfg, "") })
	s.done()
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected wrong-password failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Documents")); !os.IsNotExist(err) {
		t.Fatal("nothing may be restored after failed authentication")
	}

	// Restore the newest backup and compare with the source.
	s = useScript(t, []string{".", dest, "y"}, password)
	out = testutil.CaptureStdout(t, func() {
		if err := restore.Run(cfg, ""); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	s.done()
	if !strings.Contains(out, "Restore completed successfully.") {
		t.Fatalf("unexpected restore output: %q", out)
	}
	assertTreesEqual(t, docs, filepath.Join(dest, "Documents"))
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

	matcher, err := util.NewExcludeMatcher([]string{"*.tmp", "/Cache"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &util.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		RetentionKeep:      1,
		LogLevel:           "info",
		AuthenticationMode: util.AuthModePassword,
		Exclude:            []string{"*.tmp", "/Cache"},
		ExcludeMatcher:     matcher,
		OnUnreadableFile:   util.OnUnreadableSkip,
		Argon2:             testutil.FastArgon2Config,
	}

	// First backup: everything readable.
	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(cfg, ""); err != nil {
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
		if err := backup.Run(cfg, ""); err != nil {
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

	// Restoring the newest backup reports the skipped file; excluded files
	// were never backed up.
	dest := filepath.Join(root, "Restore")
	s = useScript(t, []string{".", dest, "y"}, password)
	out = testutil.CaptureStdout(t, func() {
		if err := restore.Run(cfg, ""); err != nil {
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

// TestNewKeysKeepOldBackupsRestorable chooses [K] to change the password and
// checks that the old backup still opens with the old password only, while
// the new backup opens with the new password.
func TestNewKeysKeepOldBackupsRestorable(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	backupDir := filepath.Join(root, "Backups")
	writeFile(t, filepath.Join(docs, "a.txt"), "version 1")
	cfg := &util.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		LogLevel:           "info",
		AuthenticationMode: util.AuthModePassword,
		PasswordMinLength:  12,
		Argon2:             testutil.FastArgon2Config,
	}
	const newPassword = "a brand new password"

	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(cfg, ""); err != nil {
			t.Fatalf("backup 1: %v", err)
		}
	})
	s.done()
	infos, _ := catalog.Inventory(backupDir)
	oldRun := infos[0].Header.RunID
	oldKeys := infos[0].Header.KeySet.ID

	writeFile(t, filepath.Join(docs, "a.txt"), "version 2")
	s = useScript(t, []string{"k"}, newPassword, newPassword)
	out := testutil.CaptureStdout(t, func() {
		if err := backup.Run(cfg, ""); err != nil {
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
	s = useScript(t, []string{oldRun, oldDest, "y"}, newPassword, newPassword, newPassword)
	var err error
	testutil.CaptureStdout(t, func() { err = restore.Run(cfg, "") })
	s.done()
	if err == nil {
		t.Fatal("new password must not open the old backup")
	}
	s = useScript(t, []string{oldRun, oldDest, "y"}, password)
	testutil.CaptureStdout(t, func() {
		if err := restore.Run(cfg, ""); err != nil {
			t.Fatalf("restore old: %v", err)
		}
	})
	s.done()
	if data, _ := os.ReadFile(filepath.Join(oldDest, "Documents", "a.txt")); string(data) != "version 1" {
		t.Fatalf("old backup restored %q", data)
	}

	// The newest backup opens with the new password.
	newDest := filepath.Join(root, "RestoreNew")
	s = useScript(t, []string{".", newDest, "y"}, newPassword)
	testutil.CaptureStdout(t, func() {
		if err := restore.Run(cfg, ""); err != nil {
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
	cfg := &util.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    filepath.Join(root, "Backups"),
		SplitSizeMB:        1,
		LogLevel:           "info",
		AuthenticationMode: util.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
	}

	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(cfg, ""); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
	s.done()

	// The next backup reuses the existing keys, so only their password
	// unlocks them; a different password is rejected three times.
	s = useScript(t, []string{"y"}, "another password", "another password", "another password")
	var err error
	testutil.CaptureStdout(t, func() { err = backup.Run(cfg, "") })
	s.done()
	if err == nil || !strings.Contains(err.Error(), "Too many wrong password attempts") {
		t.Fatalf("expected existing keys to reject another password, got %v", err)
	}
}
