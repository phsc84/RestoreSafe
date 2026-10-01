package verify

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/job"
	"RestoreSafe/internal/workflow/unlock"
	"context"
	"errors"
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
	infos := fixtureInfos(t, fx)
	items := buildVerifyPreflight(infos, infos)
	if len(items) != 1 || items[0].Err != nil || items[0].PartCount != fx.Parts || items[0].TotalSizeBytes <= 0 {
		t.Fatalf("unexpected preflight item: %+v", items)
	}

	orphan := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "D", ChainID: "ABC123", Date: "2026-03-14", DiffNumber: 1}}
	if items := buildVerifyPreflight([]catalog.SetInfo{orphan}, infos); items[0].Err == nil || !strings.Contains(items[0].Err.Error(), "is missing") {
		t.Fatalf("expected missing-base error, got %+v", items[0])
	}
}

func TestVerifyDifferentialRestorePoint(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	if err := os.WriteFile(filepath.Join(fx.SrcDir, "nested", "small.txt"), []byte("changed content"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := fixtureInfos(t, fx)
	selected, err := job.SelectSets(infos, []naming.BackupEntry{diff})
	if err != nil {
		t.Fatal(err)
	}

	items := buildVerifyPreflight(selected, infos)
	if items[0].Err != nil || items[0].Base == nil || items[0].Base.Entry != fx.Entry {
		t.Fatalf("differential must find its full backup: %+v", items[0])
	}
	out := testutil.CaptureStdout(t, func() {
		_, err = verifySelectedEntries(context.Background(), nil, selected, infos, fx.BackupDir, unlock.MasterKeys{fx.KeySet.ID: fx.Master}, logging.NewConsoleLogger("info", nil))
	})
	if err != nil {
		t.Fatalf("verify differential: %v", err)
	}
	if !strings.Contains(out, "2 file(s), 1 directory(s)") || !strings.Contains(out, "Reading unchanged files from the full backup") {
		t.Fatalf("expected the complete restore point to be verified: %q", out)
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

	entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20"}
	items := []verifyPreflightItem{
		{Entry: entry, PartCount: 2, TotalSizeBytes: 2048},
		{Entry: naming.BackupEntry{DirectoryName: "Bad", ChainID: "ABC123", Date: "2026-03-20"}, Err: errors.New("Backup set is incomplete")},
	}
	var sb strings.Builder
	interact.WriteReport(&sb, verifyPreflightReport(&config.Config{LogLevel: "info"}, t.TempDir(), items, true, false, func() error { return errors.New("absent") }))
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
	log, _ := logging.NewLogger(logPath, "info", nil)

	var err error
	output := testutil.CaptureStdout(t, func() {
		infos := fixtureInfos(t, fx)
		err = runVerifyOperation(context.Background(), &interacttest.Script{}, infos, infos, fx.BackupDir, logPath, unlock.MasterKeys{fx.KeySet.ID: fx.Master}, log, 0)
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
	wrong, _ := cryptox.RandomBytes(cryptox.KeyLen)
	var err error
	testutil.CaptureStdout(t, func() {
		_, err = verifyEntry(context.Background(), nil, fx.Entry, nil, fx.BackupDir, wrong, logging.NewConsoleLogger("info", nil))
	})
	if err == nil || !strings.Contains(err.Error(), "corrupted or modified") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
}

func TestVerifySelectedEntriesProcessesMultipleEntries(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	fx.CreateBackupInDir(t, naming.BackupEntry{DirectoryName: "Second", ChainID: "SEC001", Date: "2026-03-15"})
	infos := fixtureInfos(t, fx)
	if len(infos) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(infos))
	}
	var err error
	output := testutil.CaptureStdout(t, func() {
		_, err = verifySelectedEntries(context.Background(), nil, infos, infos, fx.BackupDir, unlock.MasterKeys{fx.KeySet.ID: fx.Master}, logging.NewConsoleLogger("info", nil))
	})
	if err != nil || strings.Count(output, "successfully verified") != 2 {
		t.Fatalf("expected both sets verified, err=%v output=%q", err, output)
	}
}

func TestVerifyEntryReturnsErrorWhenNoPartsFound(t *testing.T) {
	t.Parallel()
	entry := naming.BackupEntry{DirectoryName: "Ghost", ChainID: "GHO001", Date: "2026-03-14"}
	_, err := verifyEntry(context.Background(), nil, entry, nil, t.TempDir(), make([]byte, 32), logging.NewConsoleLogger("info", nil))
	if err == nil || !strings.Contains(err.Error(), "No part files found") {
		t.Fatalf("expected no-parts error, got %v", err)
	}
}

func TestRunReturnsErrorWhenBackupDirNotFound(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{BackupDirectory: filepath.Join(t.TempDir(), "does-not-exist")}
	req := Request{Sets: []naming.BackupEntry{{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-14"}}}
	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", req); err == nil || !strings.Contains(err.Error(), "Failed to scan backup directory") {
		t.Fatalf("expected scan-error message, got: %v", err)
	}
}

func TestRunRejectsEmptyAndUnknownRequests(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	cfg := &config.Config{BackupDirectory: fx.BackupDir}

	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", Request{}); err == nil || !strings.Contains(err.Error(), "No backup chosen") {
		t.Fatalf("empty request: got %v", err)
	}
	unknown := Request{Sets: []naming.BackupEntry{{DirectoryName: "Other", ChainID: "ZZZ999", Date: "2026-03-14"}}}
	if err := Run(context.Background(), &interacttest.Script{}, cfg, "", unknown); err == nil || !strings.Contains(err.Error(), "no longer in the backup directory") {
		t.Fatalf("unknown backup: got %v", err)
	}
}

func TestRunReturnsErrorWhenStartIsNotAnswered(t *testing.T) {
	t.Parallel()
	fx := testutil.NewBackupFixture(t, []byte("pw"))

	var out strings.Builder
	runErr := Run(context.Background(), &interacttest.Script{Out: &out}, &config.Config{BackupDirectory: fx.BackupDir, LogLevel: "info"}, "", Request{Sets: []naming.BackupEntry{fx.Entry}})
	if runErr == nil {
		t.Fatal("expected an error when the start prompt gets no answer, got nil")
	}
}
