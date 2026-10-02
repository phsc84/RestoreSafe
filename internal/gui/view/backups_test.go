package view

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/testutil/scenario"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func backupsOf(t *testing.T, c scenario.Condition) (BackupsPage, *health.Snapshot, scenario.Scenario) {
	t.Helper()
	sc := scenario.Build(t, c)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	return BackupsOf(&s, sc.Config, nil, AllFolders, sc.Now), &s, sc
}

// rowsByFolder returns the rows of all groups by folder.
func rowsByFolder(p BackupsPage) map[string]BackupRow {
	rows := map[string]BackupRow{}
	for _, g := range p.Groups {
		for _, r := range g.Rows {
			rows[r.Folder] = r
		}
	}
	return rows
}

func TestBackupsOfAProtectedDirectory(t *testing.T) {
	t.Parallel()
	p, _, _ := backupsOf(t, scenario.Protected)
	if p.Empty != nil || len(p.Groups) != 1 || !p.Groups[0].Expanded {
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
	if p.Retention == nil || p.Retention.Button == nil {
		t.Fatalf("retention %+v", p.Retention)
	}
	if len(p.Filters) != 3 || p.Filters[0].Text != "All folders" || p.Filter != 0 {
		t.Fatalf("filters %+v", p.Filters)
	}
	checkWriting(t, p)
}

func TestBackupsShowTheProblems(t *testing.T) {
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
			p, _, _ := backupsOf(t, tc.condition)
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

func TestBackupsEmptyAndFilter(t *testing.T) {
	t.Parallel()
	p, _, _ := backupsOf(t, scenario.Empty)
	if p.Empty == nil || p.Empty.Button.Action != ActionBackUp || len(p.Groups) != 0 {
		t.Fatalf("empty page %+v", p)
	}

	sc := scenario.Build(t, scenario.Protected)
	sc.Config.SourceDirectories = sc.Config.SourceDirectories[:1] // Pics is no longer configured
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	all := BackupsOf(&s, sc.Config, nil, AllFolders, sc.Now)
	last := all.Filters[len(all.Filters)-1]
	if last.Text != "Old: Pics" || last.Folder != "Pics" {
		t.Fatalf("filters %+v", all.Filters)
	}
	only := BackupsOf(&s, sc.Config, nil, "Pics", sc.Now)
	rows := rowsByFolder(only)
	if len(rows) != 1 || rows["Pics"].Folder != "Pics" || only.Filters[only.Filter].Folder != "Pics" {
		t.Fatalf("filtered rows %+v", rows)
	}
}

func TestBackupsListAFailedRunByItsLog(t *testing.T) {
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
	p := BackupsOf(&s, sc.Config, nil, AllFolders, sc.Now)
	if len(p.Groups) != 2 || p.Groups[0].RunID != "FAIL01" || !strings.HasSuffix(p.Groups[0].Header, "Backup failed") || p.Groups[0].Tone != ToneError {
		t.Fatalf("the failed run comes first: %+v", p.Groups)
	}
	bar := SelectionOf(p, "FAIL01", "")
	if bar.Restore.Enabled || !strings.Contains(bar.Text, "The log says why") {
		t.Fatalf("selection of a failed run %+v", bar)
	}
	o := OverviewOf(&s, sc.Config, sc.Now)
	if !strings.Contains(o.LastBackup.Note, "failed") || o.LastBackup.NoteTone != ToneError {
		t.Fatalf("the Last backup card tells of the failed run: %+v", o.LastBackup)
	}
}

func TestBackupsSelection(t *testing.T) {
	t.Parallel()
	p, _, _ := backupsOf(t, scenario.Protected)
	if bar := SelectionOf(p, "", ""); bar.Text != "Select a backup to restore or verify it." || bar.Restore.Enabled {
		t.Fatalf("no selection %+v", bar)
	}
	g := p.Groups[0]
	bar := SelectionOf(p, g.RunID, "")
	if !bar.Restore.Enabled || !bar.Verify.Enabled || len(bar.Sets) != 2 || !strings.HasPrefix(bar.Text, "Backup of ") {
		t.Fatalf("run selection %+v", bar)
	}
	set := g.Rows[0].Set
	bar = SelectionOf(p, g.RunID, set)
	if len(bar.Sets) != 1 || bar.Sets[0] != set || !strings.Contains(bar.Text, ", full backup of ") {
		t.Fatalf("set selection %+v", bar)
	}

	bm, _, _ := backupsOf(t, scenario.BaseMissing)
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
}

func TestBackupsShowTheRunningVerification(t *testing.T) {
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
	p := BackupsOf(&s, sc.Config, m.Current(), AllFolders, sc.Now)
	if st := rowsByFolder(p)["Docs"].Status; st.Text != "Verifying, 25%" {
		t.Fatalf("running status %+v", st)
	}
}

func TestVerifyConfirm(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local)
	full := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-01"}
	p := interact.VerifyPlan{
		Sets: []interact.SetPlan{
			{Set: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-30", DiffNumber: 3}, Base: full},
			{Set: naming.BackupEntry{DirectoryName: "Pics", ChainID: "DEF456", Date: "2026-09-30"}},
		},
		Bytes: 97 << 30,
	}
	c := VerifyConfirm(p, "today, 09:12", now)
	want := "RestoreSafe reads 2 folders, decrypts the backups and checks every file against its checksum. For the differentials it also reads their full backup of 1 Sep. Nothing is written. About 97 GB to read."
	if c.Instruction != "Verify today, 09:12?" || c.Content != want || c.Yes != "Verify…" {
		t.Fatalf("confirm %+v", c)
	}
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
	if LogPaneTitle("today, 09:12", "2026-09-30_QRS321.log") != "Log of today, 09:12 (2026-09-30_QRS321.log)" {
		t.Fatal("log title")
	}
}
