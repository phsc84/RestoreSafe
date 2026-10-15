package verify

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/testutil/inventory"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/job"
)

func TestVerifyPlanDescribesTheVerification(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := inventory.Read(t, fx.BackupDir)
	selected, err := job.SelectSets(infos, []naming.BackupEntry{diff})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{BackupDirectory: fx.BackupDir}
	noYubiKey := func() error { return nil }
	planFor := func(inventory []catalog.SetInfo) interact.VerifyPlan {
		items := job.SelectionPreflight(selected, inventory)
		return verifyPlan(items, fx.KeySet, verifyPreflightReport(cfg, fx.BackupDir, items, config.AuthModePassword, noYubiKey))
	}

	p := planFor(infos)
	if len(p.Sets) != 1 || p.Sets[0].Base != fx.Entry || p.Bytes != p.Sets[0].Bytes || p.HasErrors() {
		t.Fatalf("the differential must be verified with its full backup: %+v", p)
	}
	if p.Unlock.Methods != config.AuthModePassword.Label() || p.Details.Title != "Verification preflight" {
		t.Fatalf("unexpected unlock or details: %+v", p)
	}

	// Without its full backup, the differential cannot be verified.
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if err := os.Remove(part); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := catalog.Inventory(fx.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	p = planFor(inventory)
	if !p.HasErrors() || p.Issues[0].Code != interact.CodeBaseMissing || p.Sets[0].Problem == "" || p.Bytes != 0 {
		t.Fatalf("a missing full backup must block the verification: %+v", p)
	}
}

func TestSelectionPreflightUsesInventory(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("verify-preflight-pass"))
	infos := inventory.Read(t, fx.BackupDir)
	items := job.SelectionPreflight(infos, infos)
	if len(items) != 1 || items[0].Err != nil || items[0].PartCount != fx.Parts || items[0].TotalSizeBytes <= 0 {
		t.Fatalf("unexpected preflight item: %+v", items)
	}

	orphan := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "D", ChainID: "ABC123", Date: "2026-03-14", DiffNumber: 1}}
	if items := job.SelectionPreflight([]catalog.SetInfo{orphan}, infos); items[0].Err == nil || !strings.Contains(items[0].Err.Error(), "is missing") {
		t.Fatalf("expected missing-base error, got %+v", items[0])
	}
}

func TestValidateVerifyPreflight(t *testing.T) {
	t.Parallel()

	if err := validateVerifyPreflight([]job.SelectionItem{{}, {}}); err != nil {
		t.Fatalf("expected no error for valid verify preflight, got %v", err)
	}
	err := validateVerifyPreflight([]job.SelectionItem{{}, {Err: errors.New("broken")}})
	if err == nil || !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected verify preflight error: %v", err)
	}
}

func TestVerifyPreflightReportShowsItemsSizeAndYubiKeyStatus(t *testing.T) {
	t.Parallel()

	entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20"}
	items := []job.SelectionItem{
		{Entry: entry, PartCount: 2, TotalSizeBytes: 2048},
		{Entry: naming.BackupEntry{DirectoryName: "Bad", ChainID: "ABC123", Date: "2026-03-20"}, Err: errors.New("Backup set is incomplete")},
	}
	var sb strings.Builder
	interacttest.WriteReport(&sb, verifyPreflightReport(&config.Config{LogLevel: "info"}, t.TempDir(), items, config.AuthModePasswordYubiKey, func() error { return errors.New("absent") }))
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
