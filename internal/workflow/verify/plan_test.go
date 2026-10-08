package verify

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"os"
	"testing"
)

func TestVerifyPlanDescribesTheVerification(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := fixtureInfos(t, fx)
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
