package restore

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/job"
	"RestoreSafe/internal/workflow/unlock"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureInfos(t *testing.T, fx *testutil.BackupFixture) []catalog.SetInfo {
	t.Helper()
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	return infos
}

func masterKeys(fx *testutil.BackupFixture) unlock.MasterKeys {
	return unlock.MasterKeys{fx.KeySet.ID: append([]byte(nil), fx.Master...)}
}

// assertTreesEqual compares file contents and directory structure.
func assertTreesEqual(t *testing.T, want, got string) {
	t.Helper()
	err := filepath.WalkDir(want, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(want, path)
		other := filepath.Join(got, rel)
		if d.IsDir() {
			if fi, err := os.Stat(other); err != nil || !fi.IsDir() {
				return fmt.Errorf("directory %s missing in restore", rel)
			}
			return nil
		}
		a, _ := os.ReadFile(path)
		b, err := os.ReadFile(other)
		if err != nil || !bytes.Equal(a, b) {
			return fmt.Errorf("file %s differs after restore", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunRestoreOperationRestoresFixture(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("restore-pw"))
	infos := fixtureInfos(t, fx.BackupFixture)
	logPath := filepath.Join(t.TempDir(), "restore.log")
	var out testutil.Output
	log, _ := logging.NewLogger(logPath, "info", &out)

	op := &operation{selected: infos, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, logPath: logPath, masters: masterKeys(fx.BackupFixture), log: log}
	err := op.run(context.Background(), &interacttest.Script{Out: &out}, 0)
	log.Close()
	output := out.String()
	if err != nil {
		t.Fatalf("runRestoreOperation: %v", err)
	}
	if !strings.Contains(output, "Restore completed successfully.") || !strings.Contains(output, "successfully restored and checked") {
		t.Fatalf("unexpected output: %q", output)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
}

func TestRestoreSelectedEntriesRestoresDifferential(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "nested", "small.txt"), []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := fixtureInfos(t, fx.BackupFixture)
	selected, err := job.SelectSets(infos, []naming.BackupEntry{diff})
	if err != nil {
		t.Fatal(err)
	}

	op := &operation{selected: selected, inventory: infos, backupDir: fx.BackupDir, restorePath: fx.RestoreRoot, masters: masterKeys(fx.BackupFixture), log: logging.NewConsoleLogger("info", nil)}
	_, err = op.restoreAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("restore differential: %v", err)
	}
	assertTreesEqual(t, fx.SrcDir, filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName))
}

func TestRestoreEntryRejectsWrongKey(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("right"))
	wrong, _ := cryptox.RandomBytes(cryptox.KeyLen)

	_, err := entryOperation(fx.BackupDir, fx.RestoreRoot).restoreEntry(context.Background(), nil, fx.Entry, nil, wrong)
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}

func TestRestoreEntryRefusesExistingDestination(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	if err := os.Mkdir(filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := entryOperation(fx.BackupDir, fx.RestoreRoot).restoreEntry(context.Background(), nil, fx.Entry, nil, fx.Master)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-destination error, got %v", err)
	}
}

func TestRestoreEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}
	_, err := entryOperation(t.TempDir(), t.TempDir()).restoreEntry(context.Background(), nil, entry, nil, make([]byte, 32))
	if err == nil || !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got %v", err)
	}
}

func TestRunReturnsErrorWhenBackupDirNotFound(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{BackupDirectory: filepath.Join(t.TempDir(), "does-not-exist")}
	req := Request{Sets: []naming.BackupEntry{{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}}, Destination: t.TempDir()}
	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", req); err == nil || !strings.Contains(err.Error(), "Failed to scan backup directory") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func TestRunRejectsIncompleteRequests(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	cfg := &config.Config{BackupDirectory: fx.BackupDir}
	dest := filepath.Join(t.TempDir(), "restore")

	for _, tc := range []struct {
		name string
		req  Request
		want string
	}{
		{"no destination", Request{Sets: []naming.BackupEntry{fx.Entry}}, "No restore destination chosen"},
		{"no backup", Request{Destination: dest}, "No backup chosen"},
		{"unknown backup", Request{Sets: []naming.BackupEntry{{DirectoryName: "Other", ChainID: "ZZZ999", Date: "2026-03-14"}}, Destination: dest}, "no longer in the backup directory"},
	} {
		err := Run(context.Background(), &interacttest.Script{Out: &bytes.Buffer{}}, cfg, "", tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error with %q, got %v", tc.name, tc.want, err)
		}
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("nothing may be written for a rejected request")
	}
}

func TestRunReturnsErrorWhenStartIsNotAnswered(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("prompt-pw"))

	var out strings.Builder
	req := Request{Sets: []naming.BackupEntry{fx.Entry}, Destination: filepath.Join(t.TempDir(), "restore")}
	runErr := Run(context.Background(), &interacttest.Script{Out: &out}, &config.Config{BackupDirectory: fx.BackupDir}, "", req)
	if runErr == nil {
		t.Fatal("expected an error when the start prompt gets no answer, got nil")
	}
}

// entryOperation is a restore from backupDir into restorePath that logs to
// nowhere, for tests of single sets.
func entryOperation(backupDir, restorePath string) *operation {
	return &operation{backupDir: backupDir, restorePath: restorePath, log: logging.NewConsoleLogger("info", nil)}
}
