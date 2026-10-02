package view

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"strings"
	"testing"
	"time"
)

// runningBackup is a backup of samplePlan's folders, confirmed at planNow.
func runningBackup() (*flow.Machine, *interact.BackupPlan) {
	m := &flow.Machine{}
	p := samplePlan()
	m.Start(flow.OpBackup)
	m.PlanShown(p)
	m.Confirmed(planNow)
	return m, m.Current().Plan
}

func trail(c ProgressCard) string {
	var parts []string
	for _, s := range c.Steps {
		mark := map[StepState]string{StepWaiting: "", StepCurrent: "*", StepDone: "+"}[s.State]
		parts = append(parts, mark+s.Text)
	}
	return strings.Join(parts, " > ")
}

func TestProgressCardFollowsTheSteps(t *testing.T) {
	t.Parallel()
	m, _ := runningBackup()
	r := m.Current()

	c := ProgressCardOf(r, planNow)
	if c.Title != "Backing up" || trail(c) != "*Unlock keys > Back up > Verify > Clean up" {
		t.Fatalf("unlocking: %q %q", c.Title, trail(c))
	}
	if c.Fraction != -1 || c.Line != "Unlocking keys… Follow the Windows Security prompt." {
		t.Fatalf("unlocking with a YubiKey: %+v", c)
	}

	step := interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 2, Item: "Documents", Total: 1000 << 20}
	for i := range 12 {
		step.Done = int64(i) * 50 << 20
		m.Progressed(step, planNow.Add(time.Duration(i)*time.Second))
	}
	c = ProgressCardOf(r, planNow.Add(11*time.Second))
	if trail(c) != "+Unlock keys > *Back up 1 of 2 > Verify > Clean up" || c.Line != "Documents · differential 4" {
		t.Fatalf("backing up: %q %q", trail(c), c.Line)
	}
	if c.Bytes != "550 MB of 1000 MB" || c.Speed != "50 MB/s" || c.Left != "Less than a minute left" {
		t.Fatalf("bytes %q, speed %q, left %q", c.Bytes, c.Speed, c.Left)
	}

	m.Progressed(interact.Progress{Phase: interact.PhaseVerifying, Index: 1, Count: 2, Item: "Documents", Total: 10}, planNow.Add(20*time.Second))
	c = ProgressCardOf(r, planNow.Add(20*time.Second))
	if trail(c) != "+Unlock keys > +Back up > *Verify 1 of 2 > Clean up" || c.Line != "Verifying Documents" {
		t.Fatalf("verifying: %q %q", trail(c), c.Line)
	}

	m.Cancelling()
	c = ProgressCardOf(r, planNow.Add(21*time.Second))
	if c.Cancel.Enabled || c.Cancel.Text != "Cancelling…" || c.Fraction != -1 {
		t.Fatalf("cancelling: %+v", c)
	}
	checkWriting(t, c)
}

func TestProgressCardTimeLeftNeedsTenSeconds(t *testing.T) {
	t.Parallel()
	m, _ := runningBackup()
	step := interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 1, Item: "Pictures", Total: 10 << 30}
	for i := range 6 {
		step.Done = int64(i) * 10 << 20
		m.Progressed(step, planNow.Add(time.Duration(i)*time.Second))
	}
	if c := ProgressCardOf(m.Current(), planNow.Add(5*time.Second)); c.Left != "" || c.Speed == "" || c.Line != "Pictures · full backup" {
		t.Fatalf("after 5 s: left %q, speed %q, line %q", c.Left, c.Speed, c.Line)
	}
	for i := 6; i <= 12; i++ {
		step.Done = int64(i) * 10 << 20
		m.Progressed(step, planNow.Add(time.Duration(i)*time.Second))
	}
	if c := ProgressCardOf(m.Current(), planNow.Add(12*time.Second)); !strings.HasPrefix(c.Left, "About ") {
		t.Fatalf("after 12 s: left %q", c.Left)
	}
}

func TestProgressCardOfNewKeysAndVerify(t *testing.T) {
	t.Parallel()
	m := &flow.Machine{}
	p := samplePlan()
	p.Keys = interact.KeyPlan{New: true, Password: true}
	p.VerifyAfter = false
	m.Start(flow.OpBackup)
	m.PlanShown(p)
	m.Confirmed(planNow)
	if c := ProgressCardOf(m.Current(), planNow); trail(c) != "*Create keys > Back up > Clean up" || c.Line != "Creating your keys…" {
		t.Fatalf("new keys: %q %q", trail(c), c.Line)
	}

	v := &flow.Machine{}
	v.Start(flow.OpVerify)
	v.Confirmed(planNow)
	v.Progressed(interact.Progress{Phase: interact.PhaseVerifying, Index: 2, Count: 3, Item: "Docs"}, planNow)
	if c := ProgressCardOf(v.Current(), planNow); c.Title != "Verifying" || trail(c) != "+Unlock keys > *Verify 2 of 3" {
		t.Fatalf("verify: %q %q", c.Title, trail(c))
	}
}

func TestRunFoldersMirrorTheBackup(t *testing.T) {
	t.Parallel()
	m, _ := runningBackup()
	m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 2, Item: "Documents", Done: 5, Total: 200 << 20}, planNow)
	m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 2, Count: 2, Item: "Pictures", Done: 62, Total: 100}, planNow)
	f := RunFolders(m.Current())
	if f["Documents"].Text != "Done, 200 MB" || f["Documents"].Glyph != GlyphCheck {
		t.Fatalf("done folder %+v", f["Documents"])
	}
	if f["Pictures"].Text != "Backing up, 62%" {
		t.Fatalf("current folder %+v", f["Pictures"])
	}
	if _, ok := f["Old"]; ok {
		t.Fatal("a folder that is not backed up has no run state")
	}

	m.Done(flow.End{Err: errors.New("disk full")}, planNow)
	if f := RunFolders(m.Current()); f["Pictures"].Text != "Failed" || f["Pictures"].Tone != ToneError {
		t.Fatalf("failed folder %+v", f["Pictures"])
	}

	w, _ := runningBackup()
	if f := RunFolders(w.Current()); f["Documents"].Text != "Waiting" || f["Pictures"].Text != "Waiting" {
		t.Fatalf("before the first folder: %+v", f)
	}
}

func TestCancelConfirm(t *testing.T) {
	t.Parallel()
	c := CancelConfirm(flow.OpBackup, false)
	if c.Instruction != "Cancel this backup?" || c.Yes != "Cancel backup" || c.No != "Keep running" {
		t.Fatalf("cancel %+v", c)
	}
	c = CancelConfirm(flow.OpRestore, true)
	if c.Instruction != "Close RestoreSafe?" || c.Yes != "Cancel and close" || !strings.Contains(c.Content, "restored") {
		t.Fatalf("close during restore %+v", c)
	}
}

func TestProgressCardWhilePlanning(t *testing.T) {
	t.Parallel()
	m := &flow.Machine{}
	m.Start(flow.OpBackup)
	c := ProgressCardOf(m.Current(), planNow)
	if c.Line != "Preparing the backup plan…" || c.Fraction != -1 || !c.Cancel.Enabled || strings.Contains(trail(c), "*") {
		t.Fatalf("before the plan: %+v, trail %q", c, trail(c))
	}
	m.PlanShown(samplePlan())
	if c := ProgressCardOf(m.Current(), planNow); c.Line != "Check the plan, then start the backup." {
		t.Fatalf("with the plan: %q", c.Line)
	}
	if f := RunFolders(m.Current()); f["Documents"].Badge.Text != "DIFF 4" || f["Pictures"].Badge.Text != "FULL" {
		t.Fatalf("planned types %+v", f)
	}
}
