package view

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/problem"
	"RestoreSafe/internal/testutil/scenario"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// finishedBackup runs samplePlan's two folders to the end with res, err and
// facts.
func finishedBackup(res *interact.Result, err error, facts logging.RunFacts, folders int) *flow.Run {
	m, _ := runningBackup()
	for i, name := range []string{"Documents", "Pictures"}[:folders] {
		m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: i + 1, Count: 2, Item: name, Done: 10, Total: 10}, planNow)
	}
	m.Done(flow.End{Result: res, Err: err, Facts: facts}, planNow.Add(4*time.Minute))
	return m.Current()
}

func backupFacts() logging.RunFacts {
	docs, pics := "Documents_ABC123_2026-09-30_DIFF004", "Pictures_DEF456_2026-09-30_FULL"
	return logging.RunFacts{
		Backup:  &logging.Fact{Kind: logging.FactBackup, Result: logging.ResultOK, Seconds: 252},
		Sets:    map[string]logging.Fact{docs: {Set: docs, Bytes: 400 << 20}, pics: {Set: pics, Bytes: 3 << 30}},
		Verify:  map[string]logging.Fact{docs: {Result: logging.ResultOK}, pics: {Result: logging.ResultOK}},
		Cleanup: &logging.Fact{Kind: logging.FactCleanup, Result: logging.ResultOK, Removed: 6, Bytes: 41 << 30},
	}
}

func TestResultCardOfASuccessfulBackup(t *testing.T) {
	t.Parallel()
	c := ResultCardOf(finishedBackup(&interact.Result{LogPath: "x.log"}, nil, backupFacts(), 2))
	if c == nil || c.Tone != ToneSuccess || c.Title != "2 folders backed up" {
		t.Fatalf("card %+v", c)
	}
	want := "3.4 GB in 4 min. Verified. Removed 6 old backups (41 GB)."
	if len(c.Lines) != 1 || c.Lines[0] != want {
		t.Fatalf("lines %q, want %q", c.Lines, want)
	}
	if c.Log == nil || c.Details != nil || c.Done.Action != ActionDismiss {
		t.Fatalf("buttons %+v %+v", c.Log, c.Details)
	}
	checkWriting(t, c)
}

func TestResultCardExplainsWarnings(t *testing.T) {
	t.Parallel()
	facts := backupFacts()
	docs := "Documents_ABC123_2026-09-30_DIFF004"
	facts.Sets[docs] = logging.Fact{Set: docs, Bytes: 1, Skipped: 2}
	facts.Verify["Pictures_DEF456_2026-09-30_FULL"] = logging.Fact{Result: logging.ResultFailed}
	facts.Cleanup = nil
	c := ResultCardOf(finishedBackup(&interact.Result{Warnings: 3}, nil, facts, 2))
	if c.Tone != ToneWarning || c.Title != "2 folders backed up with 3 warnings" {
		t.Fatalf("card %+v", c)
	}
	want := []string{
		"3.0 GB in 4 min.",
		"2 files in Documents couldn't be read and weren't backed up. Older backups of Documents are kept.",
		"The verification of Pictures found an error. No old backups were removed.",
		"1 other warning is in the log.",
	}
	if strings.Join(c.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q\nwant  %q", c.Lines, want)
	}
	checkWriting(t, c)
}

func TestResultCardTellsSkippedFromStaleFiles(t *testing.T) {
	t.Parallel()
	facts := backupFacts()
	docs, pics := "Documents_ABC123_2026-09-30_DIFF004", "Pictures_DEF456_2026-09-30_FULL"
	facts.Sets[docs] = logging.Fact{Set: docs, Bytes: 1, Skipped: 1, Stale: 2}
	facts.Sets[pics] = logging.Fact{Set: pics, Bytes: 1, Stale: 1}
	c := ResultCardOf(finishedBackup(&interact.Result{Warnings: 2}, nil, facts, 2))
	want := []string{
		"1 file in Documents couldn't be read and wasn't backed up. 2 files in Documents couldn't be read; this backup keeps their older version from the full backup. Older backups of Documents are kept.",
		"1 file in Pictures couldn't be read; this backup keeps its older version from the full backup. Older backups of Pictures are kept.",
	}
	if strings.Join(c.Lines[1:], "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q\nwant  %q", c.Lines, want)
	}
	checkWriting(t, c)
}

func TestResultCardOfAnEndBeforeTheStart(t *testing.T) {
	t.Parallel()
	for _, err := range []error{nil, interact.ErrCancelled, fmt.Errorf("unlock: %w", interact.ErrCancelled)} {
		m := &flow.Machine{}
		m.Start(flow.OpBackup)
		m.Done(flow.End{Err: err}, planNow)
		if c := ResultCardOf(m.Current()); c != nil {
			t.Fatalf("%v: no card when the backup did not start, got %+v", err, c)
		}
	}
	// A credential dialog cancelled after Start: still nothing written.
	if c := ResultCardOf(finishedBackup(nil, interact.ErrCancelled, logging.RunFacts{}, 0)); c != nil {
		t.Fatalf("cancelled credential dialog: %+v", c)
	}
}

func TestResultCardOfABlockedPlan(t *testing.T) {
	t.Parallel()
	m := &flow.Machine{}
	m.Start(flow.OpBackup)
	p := samplePlan()
	p.Issues = []interact.Issue{{Status: interact.StatusError, Text: "There isn't enough space.", Remedy: "Free up space."}}
	m.PlanShown(p)
	m.Done(flow.End{Err: problem.New("Backup preflight failed: not enough space.").WithRemedy("Free up space.")}, planNow)
	c := ResultCardOf(m.Current())
	if c == nil || c.Title != "Backup didn't start" || c.Tone != ToneError || len(c.Lines) != 1 || c.Lines[0] != "There isn't enough space." {
		t.Fatalf("card %+v", c)
	}
	if c.Details == nil || !strings.Contains(c.Detail, "Remedy") {
		t.Fatalf("details %+v %q", c.Details, c.Detail)
	}
}

func TestResultCardOfACancelledBackup(t *testing.T) {
	t.Parallel()
	m, _ := runningBackup()
	m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 2, Item: "Documents", Done: 10, Total: 10}, planNow)
	m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 2, Count: 2, Item: "Pictures", Done: 3, Total: 10}, planNow)
	m.Cancelling()
	m.Done(flow.End{Err: job.Cancelled("Backup")}, planNow)
	c := ResultCardOf(m.Current())
	want := "Documents was backed up.|The unfinished Pictures backup was removed.|No old backups were removed."
	if c == nil || c.Title != "Backup cancelled" || c.Tone != ToneNeutral || strings.Join(c.Lines, "|") != want {
		t.Fatalf("card %+v", c)
	}
	checkWriting(t, c)
}

func TestResultCardOfAFailedBackup(t *testing.T) {
	t.Parallel()
	// Pictures is in progress when the worker fails.
	r := finishedBackup(nil, problem.New(`Backup of "D:\Pics" failed: the disk is full.`).WithRemedy("Free up space."), logging.RunFacts{}, 2)
	c := ResultCardOf(r)
	want := `Backup of "D:\Pics" failed: the disk is full.|Documents was backed up.|The unfinished Pictures backup was removed.|No old backups were removed.`
	if c == nil || c.Title != "Backup failed" || strings.Join(c.Lines, "|") != want || c.Details == nil {
		t.Fatalf("card %+v\nwant lines %q", c, want)
	}
}

func TestResultCardOfAVerification(t *testing.T) {
	t.Parallel()
	m := &flow.Machine{}
	m.Start(flow.OpVerify)
	m.Confirmed(planNow)
	facts := logging.RunFacts{Verify: map[string]logging.Fact{"a": {}, "b": {}}}
	m.Done(flow.End{Result: &interact.Result{LogPath: "v.log"}, Facts: facts, LogPath: "v.log"}, planNow.Add(90*time.Second))
	c := ResultCardOf(m.Current())
	if c == nil || c.Title != "2 folders can be restored" || c.Lines[0] != "Checked 2 folders in 2 min. Every file matched its checksum." {
		t.Fatalf("card %+v", c)
	}

	// The title names the restore point; the line says the full backups
	// were checked too.
	m = &flow.Machine{}
	m.Start(flow.OpVerify)
	m.Current().What, m.Current().Whole = "today, 09:12", true
	docs := naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-30", DiffNumber: 2}
	m.VerifyPlanShown(interact.VerifyPlan{Sets: []interact.SetPlan{
		{Set: docs, Base: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-09-01"}},
		{Set: naming.BackupEntry{DirectoryName: "Pics", ChainID: "DEF456", Date: "2026-09-30"}},
	}})
	m.Confirmed(planNow)
	m.Done(flow.End{Result: &interact.Result{LogPath: "v.log"}, Facts: facts, LogPath: "v.log"}, planNow.Add(90*time.Second))
	c = ResultCardOf(m.Current())
	if c == nil || c.Tone != ToneSuccess || c.Title != "The backup of today, 09:12 can be restored" ||
		c.Lines[0] != "Checked 2 folders in 2 min, including the full backups they're based on. Every file matched its checksum." {
		t.Fatalf("card %+v", c)
	}

	// Only some folders of the backup could be verified: the title names
	// them, never the whole backup.
	m = &flow.Machine{}
	m.Start(flow.OpVerify)
	m.Current().What = "today, 09:12"
	m.VerifyPlanShown(interact.VerifyPlan{Sets: []interact.SetPlan{{Set: naming.BackupEntry{DirectoryName: "Pics", ChainID: "DEF456", Date: "2026-09-30"}}}})
	m.Confirmed(planNow)
	m.Done(flow.End{Result: &interact.Result{LogPath: "v.log"}, Facts: facts, LogPath: "v.log"}, planNow.Add(time.Minute))
	if c := ResultCardOf(m.Current()); c == nil || c.Title != "Pics from the backup of today, 09:12 can be restored" {
		t.Fatalf("part of a backup %+v", c)
	}

	// Damage found: the title says so.
	m = &flow.Machine{}
	m.Start(flow.OpVerify)
	m.Current().What = "today, 09:12"
	m.VerifyPlanShown(interact.VerifyPlan{Sets: []interact.SetPlan{{Set: docs}}})
	m.Confirmed(planNow)
	damaged := logging.RunFacts{Verify: map[string]logging.Fact{docs.String(): {Result: logging.ResultFailed}}}
	m.Done(flow.End{Err: errors.New("Verification failed: a checksum didn't match."), Facts: damaged}, planNow.Add(time.Minute))
	if c := ResultCardOf(m.Current()); c == nil || c.Tone != ToneError || c.Title != "Damage found in the backup of today, 09:12" {
		t.Fatalf("damaged %+v", c)
	}
}

func TestResultCardPointsToOtherProblems(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.BaseMissing)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	ok := ResultCard{Tone: ToneSuccess, Lines: []string{"Checked 2 folders in 2 min."}}
	c := ok
	AddProblemHint(&c, &s, sc.Config, sc.Now, true)
	if len(c.Lines) != 2 || c.Lines[1] != "Another backup has a problem; see below." {
		t.Fatalf("below %+v", c.Lines)
	}
	c = ResultCard{Tone: ToneSuccess, Lines: []string{"x"}}
	AddProblemHint(&c, &s, sc.Config, sc.Now, false)
	if c.Lines[1] != "Another backup has a problem; the Restore backup page shows it." {
		t.Fatalf("elsewhere %+v", c.Lines)
	}
	// A warning or an error card says enough itself; without a problem
	// nothing is added.
	c = ResultCard{Tone: ToneWarning, Lines: []string{"x"}}
	AddProblemHint(&c, &s, sc.Config, sc.Now, true)
	clean := scenario.Build(t, scenario.Protected)
	cs := health.TakeSnapshot(health.Params{Config: clean.Config, ConfigPath: clean.ConfigPath, Now: clean.Now})
	d := ResultCard{Tone: ToneSuccess, Lines: []string{"x"}}
	AddProblemHint(&d, &cs, clean.Config, clean.Now, true)
	if len(c.Lines) != 1 || len(d.Lines) != 1 {
		t.Fatalf("added to %+v / %+v", c.Lines, d.Lines)
	}
}

func TestSetFolderKeepsUnderscores(t *testing.T) {
	t.Parallel()
	if f := setFolder("Docs__from__C_RootA_ABC123_2026-09-30_DIFF003"); f != "Docs__from__C_RootA" {
		t.Fatalf("folder %q", f)
	}
}

func TestResultCardOfAFailedRunOffersItsLog(t *testing.T) {
	t.Parallel()
	m, _ := runningBackup()
	m.Done(flow.End{Err: errors.New("Too many wrong password attempts."), LogPath: "2026-09-30_QRS321.log"}, planNow)
	c := ResultCardOf(m.Current())
	if c == nil || c.Title != "Backup failed" || c.Log == nil {
		t.Fatalf("a failed run with a log offers it: %+v", c)
	}
}
