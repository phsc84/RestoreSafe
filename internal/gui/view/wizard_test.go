package view

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/logging"
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
	if len(got) != 3 || got[0].State != StepDone || got[1].State != StepCurrent || got[2].State != StepWaiting || got[1].Text != "2 Destination" {
		t.Fatalf("steps %+v", got)
	}
}

func TestRestorePointAndFolders(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.BaseMissing)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	if len(s.Runs) == 0 {
		t.Fatal("no runs")
	}
	run := s.Runs[0]
	if got, want := FoldersHeading(RestorePointOf(&s, run.RunID, sc.Now)), "Which folders do you want back from "+When(run.Created, sc.Now)+"?"; got != want {
		t.Fatalf("heading %q, want %q", got, want)
	}
	if got := RestorePointOf(&s, "unknown", sc.Now); got != "" {
		t.Fatalf("unknown run named %q", got)
	}
	var docsDiff, pics *FolderChoice
	for _, run := range s.Runs {
		for _, c := range RestoreFoldersOf(&s, run.RunID) {
			c := c
			switch {
			case c.Folder == "Docs" && c.Set.IsDiff():
				docsDiff = &c
			case c.Folder == "Pics":
				pics = &c
			}
		}
	}
	if docsDiff == nil || docsDiff.Enabled {
		t.Fatalf("a differential without its full backup: %+v", docsDiff)
	}
	if pics == nil || !pics.Enabled || !strings.HasPrefix(pics.About, "about ") {
		t.Fatalf("a full backup: %+v", pics)
	}
	if got := UnrestorableNote([]FolderChoice{*pics, *docsDiff}); got != "Docs can't be restored: its full backup is missing." {
		t.Fatalf("note %q", got)
	}
	if got := UnrestorableNote([]FolderChoice{*docsDiff, *docsDiff}); got != "Docs and Docs can't be restored: their full backups are missing." {
		t.Fatalf("note for two %q", got)
	}
	if got := UnrestorableNote([]FolderChoice{*pics}); got != "" {
		t.Fatalf("note without unrestorable folders %q", got)
	}
	footer := SelectionFooter([]FolderChoice{*pics, *docsDiff}, map[naming.BackupEntry]bool{pics.Set: true, docsDiff.Set: true})
	if footer != "1 folder · about "+Size(pics.Bytes) {
		t.Fatalf("footer %q: a disabled folder does not count", footer)
	}
	checkWriting(t, []FolderChoice{*pics, *docsDiff})
}

func TestDestinationChecks(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	var sets []naming.BackupEntry
	for _, c := range RestoreFoldersOf(&s, s.Runs[0].RunID) {
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
	v := RestoreCheckOf(p, "today, 09:12", now)
	if v.Summary != "1 folder from the backup of today, 09:12, into a new folder in" || v.Destination != `D:\Restore` {
		t.Fatalf("summary %q, destination %q", v.Summary, v.Destination)
	}
	checkTable(t, v.Folders, "Folder", "Read from")
	if r := v.Folders.Rows; len(r) != 1 || r[0].Cells[0].Text != "Documents" || r[0].Cells[1].Text != "Differential 3 + full backup of 1 Sep" || !strings.Contains(r[0].Tip, `D:\Restore\Documents`) {
		t.Fatalf("folders %+v", r)
	}
	if v.Space.Text != "Enough space: about 92 GB needed, 212 GB free" || v.Space.Tone != ToneSuccess {
		t.Fatalf("space %+v", v.Space)
	}
	if v.Unlock.Text != "Unlock with password + YubiKey, or recovery code" {
		t.Fatalf("unlock %q", v.Unlock.Text)
	}
	if v.Restore == nil {
		t.Fatal("a clean plan offers Restore")
	}
	full := p
	full.Sets = append([]interact.RestoreSetPlan{{SetPlan: interact.SetPlan{Set: naming.BackupEntry{DirectoryName: "Pictures", ChainID: "DEF456", Date: "2026-09-30"}}, OutputDir: `D:\Restore\Pictures`}}, p.Sets...)
	if v := RestoreCheckOf(full, "today, 09:12", now); v.Summary != "2 folders from the backup of today, 09:12, each into a new folder in" || v.Folders.Rows[0].Cells[1].Text != "Full backup" {
		t.Fatalf("two folders %+v", v)
	}
	p.Issues = []interact.Issue{{Status: interact.StatusError, Code: interact.CodeRestoreTargetExists, Text: "Restore directory already exists. Remedy: Choose another."}}
	if v := RestoreCheckOf(p, "today, 09:12", now); v.Restore != nil || len(v.Issues) != 1 || v.Issues[0].Text != "Restore directory already exists. Choose another." {
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

func TestRestoreResultNamesUnreadFiles(t *testing.T) {
	t.Parallel()
	m := restoreRun()
	docs, pics := "Docs_ABC123_2026-09-30_DIFF002", "Pics_DEF456_2026-09-30_FULL"
	facts := logging.RunFacts{Restored: map[string]logging.Fact{
		docs: {Kind: logging.FactRestore, Set: docs, Skipped: 1, Stale: 3},
		pics: {Kind: logging.FactRestore, Set: pics, Skipped: 2},
	}}
	m.Done(flow.End{Result: &interact.Result{Warnings: 1}, Facts: facts, LogPath: "x.log"}, planNow.Add(18*time.Minute))
	r := ResultCardOf(m.Current())
	want := []string{
		"Docs: 1 file isn't in this backup. It couldn't be read when the backup was made; the log names it.",
		"Docs: 3 files are restored in an older version from 1 Sep. They couldn't be read when the backup was made; the log names them.",
		"Pics: 2 files aren't in this backup. They couldn't be read when the backup was made; the log names them.",
	}
	if r == nil || r.Tone != ToneWarning || strings.Join(r.Lines[1:], "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q\nwant  %q", r.Lines, want)
	}
	checkWriting(t, r)
}
