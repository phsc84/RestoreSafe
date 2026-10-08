package e2e

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cancelAt returns a UI that cancels ctx at the first progress report of
// phase, and reports whether it did.
func cancelAt(s *script, phase interact.Phase, cancel context.CancelFunc) (*observedUI, *bool) {
	hit := new(bool)
	return &observedUI{Script: s.ui, onProgress: func(p interact.Progress) {
		if p.Phase == phase && !*hit {
			*hit = true
			cancel()
		}
	}}, hit
}

// TestCancelAtEachPhase is the cancel row of the fault-injection matrix
// (doc.go): each operation is cancelled at the start of each of its phases.
// It ends as cancelled and leaves no temporary or incomplete set; a restore
// cancelled before writing leaves no folder, one cancelled while writing is
// marked INCOMPLETE. The cleanup is the exception: retention deletes whole
// chains and is not stopped halfway, so a cancel that arrives during it is
// too late and the run completes.
func TestCancelAtEachPhase(t *testing.T) {
	phases := map[string][]interact.Phase{
		"backup":  {interact.PhaseUnlocking, interact.PhaseBackingUp, interact.PhaseVerifying, interact.PhaseCleaningUp},
		"restore": {interact.PhaseUnlocking, interact.PhaseRestoring},
		"verify":  {interact.PhaseUnlocking, interact.PhaseVerifying},
	}
	for op, list := range phases {
		for _, phase := range list {
			t.Run(op+"/"+phaseName(phase), func(t *testing.T) {
				root := t.TempDir()
				docs := filepath.Join(root, "Docs")
				writeFile(t, filepath.Join(docs, "a.txt"), strings.Repeat("restoresafe", 100_000))
				cfg := progressConfig([]string{docs}, filepath.Join(root, "Backups"))
				cfg.RetentionKeep = 1
				// Two runs, so that a third has an old chain to clean up.
				for i, pw := range [][]string{{password, password}, {password}} {
					s := useScript(t, []string{"y"}, pw...)
					if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
						t.Fatalf("backup %d: %v", i+1, err)
					}
				}

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := useScript(t, []string{"y"}, password)
				o, hit := cancelAt(s, phase, cancel)
				var err error
				dest := filepath.Join(root, "Restore")
				switch op {
				case "backup":
					err = backup.Run(ctx, o, cfg, "")
				case "restore":
					err = restore.Run(ctx, o, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: dest})
				case "verify":
					err = verify.Run(ctx, o, cfg, "", verify.Request{Sets: newestRun(t, cfg)})
				}
				if !*hit {
					t.Fatalf("no progress report of phase %d", phase)
				}
				if tooLate := phase == interact.PhaseCleaningUp; tooLate != (err == nil) || !tooLate && !errors.Is(err, context.Canceled) {
					t.Errorf("got %v; want the cancellation, or success during the cleanup", err)
				}
				infos, _ := catalog.Inventory(cfg.BackupDirectory)
				for _, info := range infos {
					if !info.Complete() {
						t.Errorf("incomplete set after the cancellation: %s: %v", info.Entry, info.Err)
					}
				}
				if temps, _ := catalog.ListTempParts(cfg.BackupDirectory); len(temps) != 0 {
					t.Errorf("temporary parts after the cancellation: %v", temps)
				}
				_, statErr := os.Stat(filepath.Join(dest, "Docs"))
				switch {
				case op == "restore" && phase == interact.PhaseUnlocking && !os.IsNotExist(statErr):
					t.Error("a restore cancelled before writing must leave no folder")
				case phase == interact.PhaseRestoring && !strings.Contains(s.out.String(), "INCOMPLETE"):
					t.Errorf("a restore cancelled while writing must be marked INCOMPLETE: %s", s.out.String())
				}
			})
		}
	}
}

func phaseName(p interact.Phase) string {
	return map[interact.Phase]string{
		interact.PhaseUnlocking: "unlocking", interact.PhaseBackingUp: "backing-up", interact.PhaseVerifying: "verifying",
		interact.PhaseCleaningUp: "cleaning-up", interact.PhaseRestoring: "restoring",
	}[p]
}
