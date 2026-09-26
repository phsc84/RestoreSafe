package verify

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
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

func TestBuildVerifyPreflightUsesInventory(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-preflight-pass"))
	items := buildVerifyPreflight(fixtureInfos(t, fx))
	if len(items) != 1 || items[0].Err != nil || items[0].PartCount != fx.Parts || items[0].TotalSizeBytes <= 0 {
		t.Fatalf("unexpected preflight item: %+v", items)
	}

	diff := catalog.SetInfo{Entry: util.BackupEntry{DirectoryName: "D", ChainID: "ABC123", Date: "2026-03-14", DiffNumber: 1}}
	if items := buildVerifyPreflight([]catalog.SetInfo{diff}); items[0].Err == nil {
		t.Fatal("expected differential to be rejected for now")
	}
}

func TestValidateVerifyPreflight(t *testing.T) {
	t.Parallel()

	if err := validateVerifyPreflight([]verifyPreflightItem{{}, {}}); err != nil {
		t.Fatalf("expected no error for valid verify preflight, got %v", err)
	}
	err := validateVerifyPreflight([]verifyPreflightItem{{}, {Err: errors.New("broken")}})
	if err == nil || !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected verify preflight error: %v", err)
	}
}

func TestPrintVerifyPreflightShowsItemsSizeAndYubiKeyStatus(t *testing.T) {
	t.Parallel()

	entry := util.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20"}
	items := []verifyPreflightItem{
		{Entry: entry, PartCount: 2, TotalSizeBytes: 2048},
		{Entry: util.BackupEntry{DirectoryName: "Bad", ChainID: "ABC123", Date: "2026-03-20"}, Err: errors.New("Backup set is incomplete")},
	}
	var sb strings.Builder
	printVerifyPreflightWithYubiKeyCheck(&sb, &util.Config{LogLevel: "info"}, t.TempDir(), items, true, false, func() error { return nil }, func() error { return errors.New("absent") })
	out := sb.String()
	for _, want := range []string{
		"  [OK] Docs_ABC123_2026-03-20_FULL (parts: 2)",
		"  [ERROR] Bad_ABC123_2026-03-20_FULL",
		"Backup size   : 2.00 KB",
		"[WARN] YubiKey not connected",
		"[ERROR] Backup set is incomplete",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got: %q", want, out)
		}
	}
}

func TestRunVerifyOperationVerifiesFixture(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-pw"))
	logPath := filepath.Join(t.TempDir(), "verify.log")
	log, _ := util.NewLogger(logPath, "info")

	var err error
	output := testutil.CaptureStdout(t, func() {
		err = runVerifyOperation(fixtureInfos(t, fx), fx.BackupDir, logPath, operation.MasterKeys{fx.KeySet.ID: fx.Master}, log, 0)
	})
	log.Close()
	if err != nil {
		t.Fatalf("runVerifyOperation: %v", err)
	}
	if !strings.Contains(output, "2 file(s), 1 directory(s)") || !strings.Contains(output, "Verification completed successfully.") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestVerifyEntryRejectsWrongKey(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("right"))
	wrong, _ := security.RandomBytes(security.KeyLen)
	var err error
	testutil.CaptureStdout(t, func() { _, err = verifyEntry(fx.Entry, fx.BackupDir, wrong, util.NewConsoleLogger("info")) })
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}

func TestVerifySelectedEntriesProcessesMultipleEntries(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	fx.CreateBackupInDir(t, util.BackupEntry{DirectoryName: "Second", ChainID: "SEC001", Date: "2026-03-15"})
	infos := fixtureInfos(t, fx)
	if len(infos) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(infos))
	}
	var err error
	output := testutil.CaptureStdout(t, func() {
		_, err = verifySelectedEntries(infos, fx.BackupDir, operation.MasterKeys{fx.KeySet.ID: fx.Master}, util.NewConsoleLogger("info"))
	})
	if err != nil || strings.Count(output, "successfully verified") != 2 {
		t.Fatalf("expected both sets verified, err=%v output=%q", err, output)
	}
}

func TestVerifyEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	entry := util.BackupEntry{DirectoryName: "Ghost", ChainID: "GHO001", Date: "2026-03-14"}
	_, err := verifyEntry(entry, t.TempDir(), make([]byte, 32), util.NewConsoleLogger("info"))
	if err == nil || !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got %v", err)
	}
}

func TestRunReturnsNilWhenNoBackupsFound(t *testing.T) {
	t.Parallel()
	output := testutil.CaptureStdout(t, func() {
		if err := Run(&util.Config{BackupDirectory: t.TempDir()}, ""); err != nil {
			t.Errorf("expected nil for empty target dir, got: %v", err)
		}
	})
	if !strings.Contains(output, "No complete backups found") {
		t.Fatalf("expected no-backups message, got: %q", output)
	}
}

func TestRunReturnsErrorWhenBackupDirNotFound(t *testing.T) {
	t.Parallel()
	cfg := &util.Config{BackupDirectory: filepath.Join(t.TempDir(), "does-not-exist")}
	if err := Run(cfg, ""); err == nil || !strings.Contains(err.Error(), "Failed to scan backup directory") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func pipeStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	fmt.Fprint(w, input)
	w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})
}

func TestRunCancelsSelectionWhenUserEntersQ(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	pipeStdin(t, "q\n")
	var runErr error
	output := testutil.CaptureStdout(t, func() { runErr = Run(&util.Config{BackupDirectory: fx.BackupDir}, "") })
	if runErr != nil || !strings.Contains(output, "Verification cancelled.") {
		t.Fatalf("expected cancel, got err=%v output=%q", runErr, output)
	}
}

func TestRunReturnsErrorWhenStartPromptClosed(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	pipeStdin(t, ".\n")
	var runErr error
	testutil.CaptureStdout(t, func() { runErr = Run(&util.Config{BackupDirectory: fx.BackupDir, LogLevel: "info"}, "") })
	if runErr == nil {
		t.Fatal("expected error when stdin closes before the start prompt, got nil")
	}
}
