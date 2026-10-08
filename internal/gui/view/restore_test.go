package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/gui/flow"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/testutil/scenario"
	"github.com/phsc84/restoresafe/internal/workflow/health"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/restore"
)

func TestRestorePointAndFolders(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.BaseMissing)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	if len(s.Runs) == 0 {
		t.Fatal("no runs")
	}
	if got := RestorePointOf(&s, s.Runs[0].RunID, sc.Now); got != When(s.Runs[0].Created, sc.Now) {
		t.Fatalf("restore point %q", got)
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
	if pics == nil || !pics.Enabled || pics.About != Size(pics.Bytes) {
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
	if got := Chosen([]FolderChoice{*pics, *docsDiff}, map[naming.BackupEntry]bool{pics.Set: true, docsDiff.Set: true}); len(got) != 1 || got[0] != pics.Set {
		t.Fatalf("chosen %v: a disabled folder does not count", got)
	}
	// The folder that can't be restored is named above the other issues.
	v := RestoreViewOf([]FolderChoice{*pics, *docsDiff}, map[naming.BackupEntry]bool{pics.Set: true}, "today, 09:12", "", nil, nil, false)
	if len(v.Issues) != 1 || v.Issues[0].Tone != ToneWarning || !strings.Contains(v.Issues[0].Text, "Docs can't be restored") {
		t.Fatalf("issues %+v", v.Issues)
	}
	checkWriting(t, []FolderChoice{*pics, *docsDiff})
}

func TestRestoreView(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	choices := RestoreFoldersOf(&s, s.Runs[0].RunID)
	checked := map[naming.BackupEntry]bool{}
	for _, c := range choices {
		checked[c.Set] = true
	}
	sets := Chosen(choices, checked)
	dest := t.TempDir()
	plan, err := restore.PlanDestination(sc.Config, s.BackupDir, s.Sets, sets, dest)
	if err != nil {
		t.Fatal(err)
	}
	v := RestoreViewOf(choices, checked, "today, 09:12", dest, &plan, nil, false)
	if v.Heading != "Restore 2 folders from the backup of today, 09:12" || !v.Start.Enabled || v.Start.Text != "&Start" || v.Cancel.Text != "Cancel" || v.Hint != "" || len(v.Issues) != 0 {
		t.Fatalf("free destination %+v", v)
	}
	for _, c := range choices {
		if cell := v.Checks[c.Set]; cell.Text != "New folder" || cell.Tone != ToneSuccess || !strings.Contains(v.Tips[c.Set], filepath.Join(dest, c.Folder)) {
			t.Fatalf("check of %s: %+v, tip %q", c.Folder, cell, v.Tips[c.Set])
		}
	}
	if v.Space.Label != "Space" || !strings.HasPrefix(v.Space.Text, "About ") || !strings.Contains(v.Space.Text, " · ") || v.Space.Tone != ToneSuccess {
		t.Fatalf("space %+v", v.Space)
	}
	if v.Unlock.Label != "Unlock" || v.Unlock.Text != "Password" {
		t.Fatalf("unlock %+v", v.Unlock)
	}

	// An existing folder blocks Start and is named once, with the remedy.
	if err := os.Mkdir(filepath.Join(dest, "Docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	plan, _ = restore.PlanDestination(sc.Config, s.BackupDir, s.Sets, sets, dest)
	v = RestoreViewOf(choices, checked, "today, 09:12", dest, &plan, nil, false)
	var docs naming.BackupEntry
	for _, c := range choices {
		if c.Folder == "Docs" {
			docs = c.Set
		}
	}
	if v.Start.Enabled || v.Checks[docs].Text != "Already exists" || v.Checks[docs].Tone != ToneError || len(v.Issues) != 1 || v.Issues[0].Text != "Choose another place, or rename or move the folder that already exists." {
		t.Fatalf("an existing folder blocks Start: %+v", v)
	}

	// Hints while there is nothing to check.
	none := map[naming.BackupEntry]bool{}
	if v := RestoreViewOf(choices, none, "today, 09:12", dest, nil, nil, false); v.Heading != "Restore from the backup of today, 09:12" || v.Hint != "Choose at least one folder." || v.Start.Enabled {
		t.Fatalf("nothing checked %+v", v)
	}
	for _, tc := range []struct{ dest, hint string }{{"", "Enter or browse"}, {`Restore`, "full path"}} {
		if v := RestoreViewOf(choices, checked, "today", tc.dest, nil, nil, false); v.Start.Enabled || !strings.Contains(v.Hint, tc.hint) {
			t.Fatalf("%q: %+v", tc.dest, v)
		}
	}
	if v := RestoreViewOf(choices, checked, "today", dest, nil, nil, true); !v.Checking || v.Start.Enabled || v.Hint != "Checking…" || v.HintTone != ToneSecondary {
		t.Fatalf("while checking %+v", v)
	}
	if v := RestoreViewOf(choices, checked, "today", dest, nil, problem.New("No access.").WithRemedy("Check the path."), false); v.HintTone != ToneError || v.Hint != "No access. Check the path." || v.Start.Enabled {
		t.Fatalf("check failed %+v", v)
	}
	checkWriting(t, v)
}

func TestRestoreSpaceAndUnlock(t *testing.T) {
	t.Parallel()
	p := interact.RestorePlan{NeededBytes: 92 << 30, FreeBytes: 212 << 30}
	if l := restoreSpaceLine(p); l.Text != "About 92 GB needed · 212 GB free" || l.Tone != ToneSuccess {
		t.Fatalf("enough %+v", l)
	}
	p.FreeBytes = 95 << 30
	if l := restoreSpaceLine(p); l.Tone != ToneWarning {
		t.Fatalf("tight %+v", l)
	}
	p.Issues = []interact.Issue{{Status: interact.StatusError, Code: interact.CodeSpaceInsufficient}}
	if l := restoreSpaceLine(p); l.Tone != ToneError {
		t.Fatalf("too little %+v", l)
	}
	p.FreeBytes = -1
	if l := restoreSpaceLine(p); l.Text != "About 92 GB needed · free space unknown" || l.Tone != ToneError {
		t.Fatalf("unknown %+v", l)
	}
	for _, tc := range []struct {
		u    interact.UnlockPlan
		want string
	}{
		{interact.UnlockPlan{Password: true}, "Password"},
		{interact.UnlockPlan{Password: true, YubiKey: true, RecoveryCode: true}, "One YubiKey touch, then your password, or recovery code"},
		{interact.UnlockPlan{YubiKey: true}, "One YubiKey touch"},
	} {
		if got := restoreUnlockText(tc.u); got != tc.want {
			t.Fatalf("unlock %+v: %q, want %q", tc.u, got, tc.want)
		}
	}
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
	if r == nil || r.Title != "2 folders restored with 1 warning" || r.Open == nil || !strings.HasPrefix(r.Lines[0], `About 3.0 GB to D:\Restore in 18 min. Every file matched`) {
		t.Fatalf("finished %+v", r)
	}

	f := restoreRun()
	f.Progressed(interact.Progress{Phase: interact.PhaseRestoring, Index: 1, Count: 2, Item: "Docs", Done: 1, Total: 2}, planNow)
	f.Done(flow.End{Err: problem.New(`Failed to restore directory "Docs": checksum mismatch.`).WithRemedy("Verify.")}, planNow)
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
