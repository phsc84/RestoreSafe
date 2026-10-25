package backup

import (
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
)

func TestChoosePlanShowsEveryPlanBeforeItStarts(t *testing.T) {
	env := newPreviewEnv(t, 0, 0)
	env.writeLargeFull(t, "AAA001", "2026-09-01")
	cfg := *env.cfg
	cfg.BackupDirectory = env.dir
	infos, err := catalog.Inventory(env.dir)
	if err != nil {
		t.Fatal(err)
	}
	sources := plan.ResolveSources(cfg.SourceDirectories, "")

	cases := []struct {
		name       string
		answers    []interact.BackupStart
		start      bool
		plansShown int
		diff       bool
		newKeys    bool
	}{
		{"as planned", []interact.BackupStart{interact.BackupAsPlanned}, true, 1, true, false},
		{"full backup instead", []interact.BackupStart{interact.BackupFull, interact.BackupAsPlanned}, true, 2, false, false},
		{"back to the automatic plan", []interact.BackupStart{interact.BackupFull, interact.BackupAutomatic, interact.BackupAsPlanned}, true, 3, true, false},
		{"new keys", []interact.BackupStart{interact.BackupNewKeys, interact.BackupAsPlanned}, true, 2, false, true},
		{"cancel after choosing full", []interact.BackupStart{interact.BackupFull, interact.BackupCancel}, false, 2, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ui := &interacttest.Script{Starts: tc.answers}
			keys, folders, start, err := choosePlan(ui, &cfg, env.dir, sources, infos, "")
			if err != nil || start != tc.start {
				t.Fatalf("start=%v err=%v, want start=%v", start, err, tc.start)
			}
			if len(ui.BackupPlans) != tc.plansShown {
				t.Fatalf("%d plans shown, want %d", len(ui.BackupPlans), tc.plansShown)
			}
			if got := folders["Docs"].IsDiff(); got != tc.diff {
				t.Fatalf("differential = %v, want %v", got, tc.diff)
			}
			if got := keys.Existing == nil; got != tc.newKeys {
				t.Fatalf("new keys = %v, want %v", got, tc.newKeys)
			}
			last := ui.BackupPlans[len(ui.BackupPlans)-1]
			if last.Folders[0].Differential != tc.diff || last.Keys.New != tc.newKeys || last.FullRequested != !tc.diff {
				t.Fatalf("the last plan shown is not the plan returned: %+v", last)
			}
		})
	}
}

func TestBackupPlanDescribesTheRun(t *testing.T) {
	env := newPreviewEnv(t, 1, 0)
	full := env.writeLargeFull(t, "AAA001", "2026-09-01")
	cfg := *env.cfg
	cfg.BackupDirectory = env.dir
	cfg.VerifyAfterBackup = true
	infos, err := catalog.Inventory(env.dir)
	if err != nil {
		t.Fatal(err)
	}
	sources := plan.ResolveSources(cfg.SourceDirectories, "")
	ui := &interacttest.Script{Starts: []interact.BackupStart{interact.BackupFull, interact.BackupCancel}}
	if _, _, _, err := choosePlan(ui, &cfg, env.dir, sources, infos, ""); err != nil {
		t.Fatal(err)
	}

	auto, forced := ui.BackupPlans[0], ui.BackupPlans[1]
	folder := auto.Folders[0]
	if folder.Name != "Docs" || !folder.Differential || folder.DiffNumber != 1 || folder.Base != full || folder.Reason == "" {
		t.Fatalf("unexpected folder plan: %+v", folder)
	}
	if folder.AllBytes != auto.AllBytes || auto.NeededBytes > auto.AllBytes || auto.FreeBytes <= 0 {
		t.Fatalf("unexpected sizes: %+v", auto)
	}
	if auto.Keys.New || auto.Keys.Summary == "" || !auto.Keys.Password || auto.Keys.YubiKey {
		t.Fatalf("unexpected key plan: %+v", auto.Keys)
	}
	if !auto.VerifyAfter || len(auto.Removes) != 0 || auto.HasErrors() || auto.Details.Title != "Backup preflight" {
		t.Fatalf("unexpected plan: %+v", auto)
	}
	// A full backup starts a second chain, so retention (keep 1) removes the
	// first one if the run succeeds.
	if forced.Folders[0].Differential || len(forced.Removes) != 1 || forced.Removes[0].Entry != full {
		t.Fatalf("unexpected full plan: %+v", forced)
	}
}
