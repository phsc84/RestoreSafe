package restore

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"os"
	"path/filepath"
	"testing"
)

func TestRestorePlanDescribesTheRestore(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("pw"))
	diff := testutil.WriteDiffSet(t, fx.SrcDir, fx.BackupDir, fx.Entry, 1, "2026-03-20", fx.KeySet, fx.Master)
	infos := fixtureInfos(t, fx.BackupFixture)
	selected, err := job.SelectSets(infos, []naming.BackupEntry{diff})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{BackupDirectory: fx.BackupDir}
	noYubiKey := func() error { return nil }
	planFor := func() interact.RestorePlan {
		items := buildRestorePreflight(selected, infos, fx.RestoreRoot)
		details := restorePreflightReport(cfg, fx.BackupDir, fx.RestoreRoot, items, false, false, noYubiKey)
		return restorePlan(items, fx.RestoreRoot, fx.KeySet, details)
	}

	p := planFor()
	set := p.Sets[0]
	if set.Set != diff || set.Base != fx.Entry || set.Bytes != selected[0].SizeBytes+infos[len(infos)-1].SizeBytes {
		t.Fatalf("the differential must be read with its full backup: %+v", set)
	}
	if set.OutputDir != filepath.Join(fx.RestoreRoot, diff.DirectoryName) || set.OutputProblem != "" || set.Problem != "" {
		t.Fatalf("unexpected output: %+v", set)
	}
	if p.NeededBytes != set.Bytes || p.FreeBytes <= 0 || p.Destination != fx.RestoreRoot || p.HasErrors() {
		t.Fatalf("unexpected plan: %+v", p)
	}
	if p.Unlock.Methods != config.AuthModePassword.Label() || p.Unlock.RecoveryCode || p.Details.Title != "Restore preflight" {
		t.Fatalf("unexpected unlock or details: %+v", p.Unlock)
	}

	// The folder to restore into exists already.
	if err := os.MkdirAll(set.OutputDir, 0o750); err != nil {
		t.Fatal(err)
	}
	p = planFor()
	if !p.HasErrors() || p.Issues[0].Code != interact.CodeRestoreTargetExists || p.Sets[0].OutputProblem == "" {
		t.Fatalf("an existing folder must block the restore: %+v", p)
	}
}
