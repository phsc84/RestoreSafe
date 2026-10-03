package view

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/testutil/scenario"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWizardSteps(t *testing.T) {
	t.Parallel()
	got := WizardSteps(WizardDestination)
	if len(got) != 4 || got[0].State != StepDone || got[1].State != StepDone || got[2].State != StepCurrent || got[3].State != StepWaiting || got[2].Text != "3 Destination" {
		t.Fatalf("steps %+v", got)
	}
}

func TestRestorePointsAndFolders(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.BaseMissing)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	points := RestorePointsOf(&s, sc.Now)
	if len(points) == 0 {
		t.Fatal("no restore points")
	}
	var docsDiff, pics *FolderChoice
	for _, p := range points {
		for _, c := range RestoreFoldersOf(&s, p.RunID, sc.Now) {
			c := c
			switch {
			case c.Folder == "Docs" && c.Set.IsDiff():
				docsDiff = &c
			case c.Folder == "Pics":
				pics = &c
			}
		}
	}
	if docsDiff == nil || docsDiff.Enabled || docsDiff.Reason != "Its full backup is missing." {
		t.Fatalf("a differential without its full backup: %+v", docsDiff)
	}
	if pics == nil || !pics.Enabled || pics.With != "" || !strings.HasPrefix(pics.About, "about ") {
		t.Fatalf("a full backup: %+v", pics)
	}
	footer := SelectionFooter([]FolderChoice{*pics, *docsDiff}, map[naming.BackupEntry]bool{pics.Set: true, docsDiff.Set: true})
	if footer != "1 folder · about "+Size(pics.Bytes) {
		t.Fatalf("footer %q: a disabled folder does not count", footer)
	}
	checkWriting(t, points)
}

func TestDestinationChecks(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	var sets []naming.BackupEntry
	for _, c := range RestoreFoldersOf(&s, s.Runs[0].RunID, sc.Now) {
		sets = append(sets, c.Set)
	}
	dest := t.TempDir()
	plan, err := restore.PlanDestination(sc.Config, s.BackupDir, s.Sets, sets, dest)
	if err != nil {
		t.Fatal(err)
	}
	v := DestinationOf(dest, &plan, nil, false)
	if !v.Next || len(v.Folders.Rows) != 2 || v.Folders.Rows[0].Cells[2].Tone != ToneSuccess || v.Remedy != "" || v.Space == nil || v.Space.Tone != ToneSuccess {
		t.Fatalf("free destination %+v", v)
	}
	checkTable(t, v.Folders, "Folder", "Restored to", "Check")
	if err := os.Mkdir(filepath.Join(dest, "Docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	plan, _ = restore.PlanDestination(sc.Config, s.BackupDir, s.Sets, sets, dest)
	v = DestinationOf(dest, &plan, nil, false)
	exists := false
	for _, f := range v.Folders.Rows {
		exists = exists || (f.Cells[2].Tone == ToneError && f.Cells[2].Text == "Already exists")
	}
	if v.Next || !exists || v.Remedy == "" {
		t.Fatalf("an existing folder blocks Next: %+v", v)
	}
	for _, tc := range []struct{ dest, hint string }{{"", "Enter or browse"}, {`Restore`, "full path"}} {
		if v := DestinationOf(tc.dest, nil, nil, false); v.Next || !strings.Contains(v.Hint, tc.hint) {
			t.Fatalf("%q: %+v", tc.dest, v)
		}
	}
	if v := DestinationOf(dest, nil, nil, true); !v.Checking || v.Next {
		t.Fatalf("while checking %+v", v)
	}
	checkWriting(t, v)
}

func TestRestoreCheck(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local)
	p := interact.RestorePlan{
		Sets: []interact.RestoreSetPlan{{
			SetPlan:   interact.SetPlan{Set: naming.BackupEntry{DirectoryName: "Documents", ChainID: "ABC123", Date: "2026-09-30", DiffNumber: 3}, Base: naming.BackupEntry{DirectoryName: "Documents", ChainID: "ABC123", Date: "2026-09-01"}},
			OutputDir: `D:\Restore\Documents`,
		}},
		Destination: `D:\Restore`, NeededBytes: 92 << 30, FreeBytes: 212 << 30,
		Unlock: interact.UnlockPlan{Methods: "password + YubiKey", RecoveryCode: true},
	}
	v := RestoreCheckOf(p, "Today, 09:12", now)
	want := []string{"Today, 09:12\nDocuments: differential 3 + full backup of 1 Sep", "", "Enough space: about 92 GB needed, 212 GB free", "Password + YubiKey, or recovery code"}
	if p := v.Lines[1].Paths; len(p) != 1 || p[0] != `D:\Restore\Documents (new)` {
		t.Fatalf("to %q", p)
	}
	for i, l := range v.Lines {
		if l.Text != want[i] {
			t.Fatalf("line %d %q, want %q", i, l.Text, want[i])
		}
	}
	if v.Restore == nil {
		t.Fatal("a clean plan offers Restore")
	}
	p.Issues = []interact.Issue{{Status: interact.StatusError, Code: interact.CodeRestoreTargetExists, Text: "Restore directory already exists. Remedy: Choose another."}}
	if v := RestoreCheckOf(p, "Today, 09:12", now); v.Restore != nil || len(v.Issues) != 1 || v.Issues[0].Text != "Restore directory already exists. Choose another." {
		t.Fatalf("a blocked plan %+v", v)
	}
	checkWriting(t, v)
}

// restoreRun is a restore of Docs and Pics into D:\Restore, confirmed.
func restoreRun() *flow.Machine {
	m := &flow.Machine{}
	m.Start(flow.OpRestore)
	m.RestorePlanShown(interact.RestorePlan{
		Destination: `D:\Restore`, NeededBytes: 3 << 30,
		Sets: []interact.RestoreSetPlan{
			{SetPlan: interact.SetPlan{Set: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-30", DiffNumber: 2}, Base: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-01"}}, OutputDir: `D:\Restore\Docs`},
			{SetPlan: interact.SetPlan{Set: naming.BackupEntry{DirectoryName: "Pics", ChainID: "DEF456", Date: "2026-09-30"}}, OutputDir: `D:\Restore\Pics`},
		},
	})
	m.Confirmed(planNow)
	return m
}

func TestRestoreProgressAndResults(t *testing.T) {
	t.Parallel()
	m := restoreRun()
	m.Progressed(interact.Progress{Phase: interact.PhaseRestoring, Index: 1, Count: 2, Item: "Docs", Done: 1, Total: 2}, planNow)
	c := ProgressCardOf(m.Current(), planNow)
	if c.Title != "Restoring" || c.Line != "Docs · differential 2, with its full backup of 1 Sep" || trail(c) != "+Unlock keys > *Restore 1 of 2" {
		t.Fatalf("progress %q %q %q", c.Title, c.Line, trail(c))
	}
	m.Progressed(interact.Progress{Phase: interact.PhaseRestoring, Index: 2, Count: 2, Item: "Pics", Done: 5, Total: 5}, planNow)
	facts := m.Current().Facts
	m.Done(flow.End{Result: &interact.Result{Warnings: 1}, Facts: facts, LogPath: "x.log"}, planNow.Add(18*time.Minute))
	r := ResultCardOf(m.Current())
	if r == nil || r.Title != "Restore finished with 1 warning" || r.Open == nil || !strings.HasPrefix(r.Lines[0], `2 folders (about 3.0 GB) restored to D:\Restore in 18 min. Every file matched`) {
		t.Fatalf("finished %+v", r)
	}

	f := restoreRun()
	f.Progressed(interact.Progress{Phase: interact.PhaseRestoring, Index: 1, Count: 2, Item: "Docs", Done: 1, Total: 2}, planNow)
	f.Done(flow.End{Err: errors.New(`Failed to restore directory "Docs": checksum mismatch. Remedy: Verify.`)}, planNow)
	r = ResultCardOf(f.Current())
	want := `Failed to restore directory "Docs": checksum mismatch.|D:\Restore\Docs is incomplete; don't use it as a full copy.|Pics wasn't restored.|Verify this backup, or restore from an older one.`
	if r == nil || r.Title != "Restore incomplete" || r.Tone != ToneError || strings.Join(r.Lines, "|") != want {
		t.Fatalf("failed %+v", r)
	}
	checkWriting(t, r)
}
