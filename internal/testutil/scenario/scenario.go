// Package scenario builds backup directories in the conditions the user
// interface must show (GUI spec 3.5 and 11.8), for tests. Each scenario has
// real backup sets, written with password-only keys, and a run log with
// facts, damaged or extended on purpose for its condition.
package scenario

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
)

// Condition names a state of the folders and the backup directory.
type Condition string

const (
	// Protected: two folders with a complete, recent full backup each.
	Protected Condition = "Protected"
	// Empty: an empty backup directory.
	Empty Condition = "Empty"
	// Overdue: Protected, looked at after the reminder limit.
	Overdue Condition = "Overdue"
	// SkippedFiles: the newest backup of Docs misses 2 unreadable files.
	SkippedFiles Condition = "SkippedFiles"
	// BaseMissing: Docs has a differential whose full backup is gone.
	BaseMissing Condition = "BaseMissing"
	// SourceMissing: the folder Pics no longer exists.
	SourceMissing Condition = "SourceMissing"
	// BackupDirUnreachable: the backup directory is on a missing drive.
	BackupDirUnreachable Condition = "BackupDirUnreachable"
	// IncompleteNewest: an unfinished backup of Docs is newer than its
	// complete one.
	IncompleteNewest Condition = "IncompleteNewest"
	// VerifyFailed: the newest verification of Docs found damage.
	VerifyFailed Condition = "VerifyFailed"
	// Argon2Capped: an Argon2 value of the configuration was capped.
	Argon2Capped Condition = "Argon2Capped"
	// NewKeysNeeded: the configuration asks for a recovery code the keys
	// do not have.
	NewKeysNeeded Condition = "NewKeysNeeded"
	// Legacy1x: a RestoreSafe 1.x backup file is in the backup directory.
	Legacy1x Condition = "Legacy1x"
	// LeftoverTmp: a part of an interrupted backup is in the backup
	// directory.
	LeftoverTmp Condition = "LeftoverTmp"
	// FolderNotBackedUp: a third folder, Music, has no backup yet.
	FolderNotBackedUp Condition = "FolderNotBackedUp"
)

// All lists every condition.
var All = []Condition{
	Protected, Empty, Overdue, SkippedFiles, BaseMissing, SourceMissing, BackupDirUnreachable,
	IncompleteNewest, VerifyFailed, Argon2Capped, NewKeysNeeded, Legacy1x, LeftoverTmp, FolderNotBackedUp,
}

// Password unlocks the keys of every scenario.
const Password = "scenario password"

// Scenario is a backup directory in one condition, with its configuration.
type Scenario struct {
	Condition  Condition
	Config     *config.Config
	ConfigPath string
	BackupDir  string
	// Sources maps the folders' backup names to their paths.
	Sources map[string]string
	// Now is the time to look at the scenario at.
	Now time.Time
}

// builder writes the scenario.
type builder struct {
	t       testing.TB
	s       Scenario
	runID   naming.BackupID
	date    string
	written map[string]naming.BackupEntry
	facts   []logging.Fact
}

// Build creates the scenario for c in a temporary directory.
func Build(t testing.TB, c Condition) Scenario {
	t.Helper()
	root := t.TempDir()
	runID, err := naming.NewBackupID()
	if err != nil {
		t.Fatal(err)
	}
	b := &builder{
		t:       t,
		runID:   runID,
		date:    time.Now().Format("2006-01-02"),
		written: make(map[string]naming.BackupEntry),
		s: Scenario{
			Condition:  c,
			ConfigPath: filepath.Join(root, "config.yaml"),
			BackupDir:  filepath.Join(root, "Backups"),
			Sources:    make(map[string]string),
			Now:        time.Now(),
		},
	}
	writeFile(t, b.s.ConfigPath, "# scenario "+string(c)+"\n")
	if err := os.MkdirAll(b.s.BackupDir, 0o750); err != nil {
		t.Fatal(err)
	}
	b.source(root, "Docs")
	b.source(root, "Pics")
	b.s.Config = &config.Config{
		SourceDirectories:  []string{b.s.Sources["Docs"], b.s.Sources["Pics"]},
		BackupDirectory:    b.s.BackupDir,
		SplitSizeMB:        1,
		LogLevel:           "info",
		AuthenticationMode: config.AuthModePassword,
		PasswordMinLength:  config.DefaultPasswordMinLength,
		Argon2:             testutil.FastArgon2Config,
	}
	if c == Empty {
		return b.s
	}
	if c == BackupDirUnreachable {
		drive := missingDrive(t)
		b.s.BackupDir = drive + `RestoreSafe`
		b.s.Config.BackupDirectory = b.s.BackupDir
		return b.s
	}

	ks, master := testutil.NewPasswordKeySet(t, []byte(Password))
	for _, name := range []string{"Docs", "Pics"} {
		entry := naming.BackupEntry{DirectoryName: name, ChainID: b.runID, Date: b.date}
		testutil.WriteFullSet(t, b.s.Sources[name], b.s.BackupDir, entry, ks, master)
		b.written[name] = entry
	}
	skipped := 0
	if c == SkippedFiles {
		skipped = 2
	}
	for _, name := range []string{"Docs", "Pics"} {
		fact := logging.Fact{Kind: logging.FactSet, Result: logging.ResultOK, Set: b.written[name].String()}
		if name == "Docs" && skipped > 0 {
			fact.Result, fact.Skipped = logging.ResultWarnings, skipped
		}
		b.facts = append(b.facts, fact)
	}
	result := logging.ResultOK
	if skipped > 0 {
		result = logging.ResultWarnings
	}
	b.facts = append(b.facts, logging.Fact{Kind: logging.FactBackup, Result: result, Warnings: min(skipped, 1), Seconds: 3})

	switch c {
	case Overdue:
		b.s.Now = time.Now().AddDate(0, 0, config.DefaultReminderDays+3)
	case BaseMissing:
		writeFile(t, filepath.Join(b.s.Sources["Docs"], "changed.txt"), "changed after the full backup")
		testutil.WriteDiffSet(t, b.s.Sources["Docs"], b.s.BackupDir, b.written["Docs"], 1, b.date, ks, master)
		b.removeSet(b.written["Docs"])
	case SourceMissing:
		if err := os.RemoveAll(b.s.Sources["Pics"]); err != nil {
			t.Fatal(err)
		}
	case IncompleteNewest:
		newer, err := naming.NewBackupID()
		if err != nil {
			t.Fatal(err)
		}
		entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: newer, Date: b.date}
		testutil.WriteFullSet(t, b.s.Sources["Docs"], b.s.BackupDir, entry, ks, master)
		b.truncateSet(entry)
	case VerifyFailed:
		b.facts = append(b.facts, logging.Fact{Kind: logging.FactVerify, Result: logging.ResultFailed, Set: b.written["Docs"].String(), Error: "checksum mismatch in Docs/report.txt"})
	case Argon2Capped:
		b.s.Config.Argon2Notices = []string{"argon2.memory_mb 8192 exceeds the maximum 4096; 4096 will be used."}
	case NewKeysNeeded:
		b.s.Config.RecoveryCode = true
	case Legacy1x:
		writeFile(t, filepath.Join(b.s.BackupDir, "[Docs]_2025-01-01_OLD001-001.enc"), "1.x data")
	case LeftoverTmp:
		writeFile(t, filepath.Join(b.s.BackupDir, "[Docs]_ZZZ999_2025-01-01_FULL-001.enc.tmp"), "partial")
	case FolderNotBackedUp:
		b.source(root, "Music")
		b.s.Config.SourceDirectories = append(b.s.Config.SourceDirectories, b.s.Sources["Music"])
	}
	b.writeLog()
	return b.s
}

func (b *builder) source(root, name string) {
	dir := filepath.Join(root, "Sources", name)
	writeFile(b.t, filepath.Join(dir, "report.txt"), "content of "+name)
	b.s.Sources[name] = dir
}

// writeLog writes the run log with the facts of the run.
func (b *builder) writeLog() {
	log, err := logging.NewLogger(naming.LogFileName(b.s.BackupDir, b.date, b.runID), "info", io.Discard)
	if err != nil {
		b.t.Fatal(err)
	}
	log.Info("Backup started - ID: %s, date: %s, 2 source directories", b.runID, b.date)
	for _, fact := range b.facts {
		log.Fact(fact)
	}
	log.Close()
}

func (b *builder) removeSet(entry naming.BackupEntry) {
	parts, err := catalog.CollectParts(b.s.BackupDir, entry)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, part := range parts {
		if err := os.Remove(part); err != nil {
			b.t.Fatal(err)
		}
	}
}

// truncateSet cuts the trailer off the last part, so the set is
// incomplete, as after a crash.
func (b *builder) truncateSet(entry naming.BackupEntry) {
	parts, err := catalog.CollectParts(b.s.BackupDir, entry)
	if err != nil {
		b.t.Fatal(err)
	}
	last := parts[len(parts)-1]
	fi, err := os.Stat(last)
	if err != nil {
		b.t.Fatal(err)
	}
	if err := os.Truncate(last, fi.Size()-10); err != nil {
		b.t.Fatal(err)
	}
}

// missingDrive returns the root of a drive letter that does not exist, e.g.
// "Q:\". It skips the test when every letter is in use.
func missingDrive(t testing.TB) string {
	t.Helper()
	for letter := 'Z'; letter >= 'D'; letter-- {
		root := string(letter) + `:\`
		if _, err := os.Stat(root); err != nil {
			return root
		}
	}
	t.Skip("every drive letter is in use")
	return ""
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
