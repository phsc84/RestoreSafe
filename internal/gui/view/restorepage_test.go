package view

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/gui/flow"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil/scenario"
	"github.com/phsc84/restoresafe/internal/workflow/health"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

func restorePageOf(t *testing.T, c scenario.Condition) (RestorePage, *health.Snapshot, scenario.Scenario) {
	t.Helper()
	sc := scenario.Build(t, c)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	return RestorePageOf(&s, sc.Config, nil, AllFolders, sc.Now), &s, sc
}

// rowsByFolder returns the rows of all groups by folder.
func rowsByFolder(p RestorePage) map[string]BackupRow {
	rows := map[string]BackupRow{}
	for _, g := range p.Groups {
		for _, r := range g.Rows {
			rows[r.Folder] = r
		}
	}
	return rows
}

func TestRestorePageOfAProtectedDirectory(t *testing.T) {
	t.Parallel()
	p, _, _ := restorePageOf(t, scenario.Protected)
	if len(p.Groups) != 1 || !p.Groups[0].Expanded {
		t.Fatalf("one expanded run: %+v", p.Groups)
	}
	g := p.Groups[0]
	if !strings.Contains(g.Header, "2 folders") || !strings.Contains(g.Header, "new keys") || g.LogPath == "" {
		t.Fatalf("run header %q, log %q", g.Header, g.LogPath)
	}
	rows := rowsByFolder(p)
	docs := rows["Docs"]
	if docs.Badge.Text != "FULL" || docs.BasedOn != "-" || docs.Chain == "" || docs.Status.Text != "Complete" || !docs.Usable {
		t.Fatalf("Docs row %+v", docs)
	}
	if p.Retention != nil {
		t.Fatalf("the rule alone is not shown (it is on Settings): %+v", p.Retention)
	}
	// What the next backup removes is shown.
	removed := []catalog.SetInfo{{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-07-06"}, SizeBytes: 41 << 30}}
	if l := retentionLine(&health.Snapshot{Retention: removed}, time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local)); l == nil || !strings.HasPrefix(l.Text, "If your next backup succeeds, it removes ") || l.Button != nil {
		t.Fatalf("retention %+v", l)
	}
	if len(p.Filters) != 3 || p.Filters[0].Text != "All folders" || p.Filter != 0 {
		t.Fatalf("filters %+v", p.Filters)
	}
	checkWriting(t, p)
}

func TestRestorePageShowTheProblems(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		condition scenario.Condition
		folder    string
		status    string
		tone      Tone
		usable    bool
		line      string // a part of a problem or information line, "" for none
	}{
		{scenario.BaseMissing, "Docs", "Full backup missing", ToneError, false, "can't be restored"},
		{scenario.SkippedFiles, "Docs", "2 skipped files", ToneWarning, true, ""},
		{scenario.VerifyFailed, "Docs", "Damaged", ToneError, true, "is damaged"},
		{scenario.IncompleteNewest, "Docs", "Incomplete", ToneError, false, "unfinished backup of Docs"},
		{scenario.Legacy1x, "Docs", "Complete", ToneSuccess, true, "RestoreSafe 1.x"},
		{scenario.LeftoverTmp, "Docs", "Complete", ToneSuccess, true, "Leftovers of an interrupted backup"},
	} {
		t.Run(string(tc.condition), func(t *testing.T) {
			t.Parallel()
			p, _, _ := restorePageOf(t, tc.condition)
			row := rowsByFolder(p)[tc.folder]
			if row.Status.Text != tc.status || row.Status.Tone != tc.tone || row.Usable != tc.usable {
				t.Fatalf("row %+v, want %q", row, tc.status)
			}
			found := tc.line == ""
			for _, l := range p.Lines {
				found = found || strings.Contains(l.Text, tc.line)
			}
			if !found {
				t.Fatalf("no line with %q in %+v", tc.line, p.Lines)
			}
			checkWriting(t, p)
		})
	}
}

func TestRestorePageEmptyAndFilter(t *testing.T) {
	t.Parallel()
	p, _, _ := restorePageOf(t, scenario.Empty)
	// Without backups the page shows the empty list, not a separate page.
	if len(p.Groups) != 0 || len(p.Columns) == 0 {
		t.Fatalf("empty page %+v", p)
	}
	// Refresh checks again, also on an empty page; not before the first check.
	if r := p.Refresh; r.Action != ActionCheckAgain || !r.Enabled {
		t.Fatalf("refresh %+v", r)
	}
	if r := RestorePageOf(nil, nil, nil, AllFolders, time.Now()).Refresh; r.Text == "" || r.Enabled {
		t.Fatalf("refresh before the first check %+v", r)
	}

	sc := scenario.Build(t, scenario.Protected)
	sc.Config.SourceDirectories = sc.Config.SourceDirectories[:1] // Pics is no longer configured
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	all := RestorePageOf(&s, sc.Config, nil, AllFolders, sc.Now)
	last := all.Filters[len(all.Filters)-1]
	if last.Text != "Old: Pics" || last.Folder != "Pics" {
		t.Fatalf("filters %+v", all.Filters)
	}
	only := RestorePageOf(&s, sc.Config, nil, "Pics", sc.Now)
	rows := rowsByFolder(only)
	if len(rows) != 1 || rows["Pics"].Folder != "Pics" || only.Filters[only.Filter].Folder != "Pics" {
		t.Fatalf("filtered rows %+v", rows)
	}
}

func TestRestorePageListAFailedRunByItsLog(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	// Log times have a resolution of one second: the failed run comes later.
	time.Sleep(1100 * time.Millisecond)
	log, err := logging.NewLogger(filepath.Join(sc.BackupDir, "2026-09-30_FAIL01.log"), "info", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	log.Fact(logging.Fact{Kind: logging.FactBackup, Result: logging.ResultFailed, Error: "disk full"})
	log.Close()
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	p := RestorePageOf(&s, sc.Config, nil, AllFolders, sc.Now)
	if len(p.Groups) != 2 || p.Groups[0].RunID != "FAIL01" || !strings.HasSuffix(p.Groups[0].Header, "Backup failed") || p.Groups[0].Tone != ToneError {
		t.Fatalf("the failed run comes first: %+v", p.Groups)
	}
	bar := SelectionOf(p, "FAIL01", "")
	if bar.Restore.Enabled || !strings.Contains(bar.Text, "The log says why") {
		t.Fatalf("selection of a failed run %+v", bar)
	}
	o := CreatePageOf(&s, sc.Config, sc.Now)
	if !strings.Contains(o.Folders.Note, "failed") || o.Folders.NoteTone != ToneError {
		t.Fatalf("the Folders card tells of the failed run: %+v", o.Folders)
	}
}

func TestRestorePageSelection(t *testing.T) {
	t.Parallel()
	p, _, _ := restorePageOf(t, scenario.Protected)
	if bar := SelectionOf(p, "", ""); bar.Text != "Select a backup to restore or verify it." || bar.Restore.Enabled {
		t.Fatalf("no selection %+v", bar)
	}
	g := p.Groups[0]
	bar := SelectionOf(p, g.RunID, "")
	if !bar.Restore.Enabled || !bar.Verify.Enabled || len(bar.Sets) != 2 || !bar.Whole || !strings.HasPrefix(bar.Text, "Backup of ") {
		t.Fatalf("run selection %+v", bar)
	}
	set := g.Rows[0].Set
	bar = SelectionOf(p, g.RunID, set)
	if len(bar.Sets) != 1 || bar.Sets[0] != set || !strings.Contains(bar.Text, ", full backup of ") {
		t.Fatalf("set selection %+v", bar)
	}

	bm, _, _ := restorePageOf(t, scenario.BaseMissing)
	var docs BackupRow
	var run naming.BackupID
	for _, gr := range bm.Groups {
		for _, r := range gr.Rows {
			if r.Folder == "Docs" {
				docs, run = r, gr.RunID
			}
		}
	}
	bar = SelectionOf(bm, run, docs.Set)
	if bar.Restore.Enabled || bar.Restore.Reason != "Its full backup is missing." {
		t.Fatalf("a set without its full backup %+v", bar)
	}
	// Its run is not whole: a verification of it would not read Docs.
	if bar = SelectionOf(bm, run, ""); bar.Whole {
		t.Fatalf("a run with a folder that can't be verified %+v", bar)
	}
}

func TestRestorePageShowTheRunningVerification(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	var docs naming.BackupEntry
	for _, info := range s.Sets {
		if info.Entry.DirectoryName == "Docs" {
			docs = info.Entry
		}
	}
	m := &flow.Machine{}
	m.Start(flow.OpVerify)
	m.VerifyPlanShown(interact.VerifyPlan{Sets: []interact.SetPlan{{Set: docs}}})
	m.Confirmed(sc.Now)
	m.Progressed(interact.Progress{Phase: interact.PhaseVerifying, Index: 1, Count: 1, Item: "Docs", Done: 1, Total: 4}, sc.Now)
	p := RestorePageOf(&s, sc.Config, m.Current(), AllFolders, sc.Now)
	if st := rowsByFolder(p)["Docs"].Status; st.Text != "Verifying, 25%" {
		t.Fatalf("running status %+v", st)
	}
}

func TestVerifyPlan(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local)
	full := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-01"}
	p := interact.VerifyPlan{
		Sets: []interact.SetPlan{
			{Set: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-30", DiffNumber: 3}, Base: full, Bytes: 38 << 30},
			{Set: naming.BackupEntry{DirectoryName: "Pics", ChainID: "DEF456", Date: "2026-09-30"}, Bytes: 59 << 30},
		},
		Bytes:  97 << 30,
		Unlock: interact.UnlockPlan{Password: true, YubiKey: true},
	}
	v := VerifyPlanOf(p, "today, 09:12", now)
	if v.Heading != "Verify 2 folders from the backup of today, 09:12" || v.Start == nil || v.Start.Text != "&Start" || v.Cancel.Text != "Cancel" {
		t.Fatalf("plan %+v", v)
	}
	checkTable(t, v.Folders, "Folder", "Type", "About")
	if r := v.Folders.Rows; len(r) != 2 || r[0].Cells[1].Badge == nil || r[0].Cells[1].Badge.Text != "DIFF 3" || r[1].Cells[2].Text != "59 GB" {
		t.Fatalf("folders %+v", r)
	}
	if v.Read.Label != "Read" || v.Read.Text != "About 97 GB to read · nothing is written" || v.Unlock.Text != "One YubiKey touch, then your password" {
		t.Fatalf("lines %+v %+v", v.Read, v.Unlock)
	}
	if v.Note != "Every file is decrypted and checked against its checksum. Differentials are read with their full backup of 1 Sep." {
		t.Fatalf("note %q", v.Note)
	}
	two := p
	two.Sets = append([]interact.SetPlan{{Set: naming.BackupEntry{DirectoryName: "Music", ChainID: "GHI789", Date: "2026-09-30", DiffNumber: 1}, Base: naming.BackupEntry{DirectoryName: "Music", ChainID: "GHI789", Date: "2026-09-01"}}}, p.Sets...)
	if got := VerifyPlanOf(two, "today", now).Note; got != "Every file is decrypted and checked against its checksum. Differentials are read with their full backups of 1 Sep." {
		t.Fatalf("two full backups of one day: %q", got)
	}
	p.Sets[0].Problem, p.Sets[0].Remedy = "Full backup missing.", "Restore it from your copy."
	p.Issues = []interact.Issue{{Status: interact.StatusError, Text: "Full backup missing.", Remedy: "Restore it from your copy."}}
	v = VerifyPlanOf(p, "today, 09:12", now)
	if v.Start != nil || len(v.Issues) != 1 || v.Issues[0].Tone != ToneError || v.Folders.Rows[0].Cells[0].Tone != ToneError {
		t.Fatalf("a blocked plan %+v", v)
	}
	checkWriting(t, v)
}

func TestLogLines(t *testing.T) {
	t.Parallel()
	text := "[2026-09-30 09:12:03] INFO  - Backup started\r\n" +
		"[2026-09-30 09:13:27] WARN  - Skipped Docs/a.pst\n" +
		"  because it is in use\n" +
		"[2026-09-30 09:13:28] FACT  - {\"kind\":\"set\"}\n" +
		"[2026-09-30 09:16:02] ERROR - Verification failed\n" +
		"[2026-09-30 09:16:03] INFO  - Done\n"
	all := LogLinesOf(text, LogAll)
	if len(all) != 5 || all[1].Tone != ToneWarning || all[2].Tone != ToneWarning || all[3].Tone != ToneError || all[4].Tone != ToneNeutral {
		t.Fatalf("all lines %+v", all)
	}
	warn := LogLinesOf(text, LogWarnings)
	if len(warn) != 3 || !strings.Contains(warn[1].Text, "because it is in use") {
		t.Fatalf("warnings %+v", warn)
	}
	none := LogLinesOf("[2026-09-30 09:12:03] INFO  - Backup started\n", LogWarnings)
	if len(none) != 1 || none[0].Text != "No warnings or errors in this log." || none[0].Tone != ToneSecondary {
		t.Fatalf("a log without warnings says so: %+v", none)
	}
	if len(LogLinesOf("", LogWarnings)) != 0 {
		t.Fatal("a log not loaded yet shows nothing")
	}
	if LogWindowTitle("today, 09:12", "2026-09-30_QRS321.log") != "Log of today, 09:12 (2026-09-30_QRS321.log)" {
		t.Fatal("log title")
	}
}

// The run card sits on Restore backup above the list, the log window has
// its own buttons, and the list has its menu: every button that can show
// at once needs an access key of its own (GUI spec 15).
func TestRestoreBackupAccessKeysAreUnique(t *testing.T) {
	lp := LogViewerOf()
	m := RestoreMenu()
	for _, set := range [][]string{
		{buttonRestore, buttonVerify, buttonCancelRun},                                 // progress
		{buttonRestore, buttonVerify, buttonDone, buttonShowRunLog, buttonOpenFolder2}, // result
		{lp.All, lp.Warnings, lp.Open},                                                 // log window
		{m.Restore, m.Verify, m.ShowLog, m.CopyName, m.OpenFolder},                     // menu
	} {
		keys := map[rune]string{}
		for _, text := range set {
			i := strings.IndexRune(text, '&')
			if i < 0 {
				t.Fatalf("%q has no access key", text)
			}
			k := []rune(strings.ToUpper(text[i+1:]))[0]
			if other, ok := keys[k]; ok {
				t.Errorf("%q and %q share Alt+%c", text, other, k)
			}
			keys[k] = text
		}
	}
}
