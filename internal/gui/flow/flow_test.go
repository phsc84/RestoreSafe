package flow

import (
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeDialogs answers the questions as the test scripts them.
type fakeDialogs struct {
	questions    []Question
	plans        []interact.BackupPlan
	opts         []interact.BackupStartOptions
	password     func(Question) ([]byte, error)
	newPw        func() ([]byte, []byte, bool)
	start        interact.BackupStart
	confirm      []byte // the confirmation NewPassword answered
	recovery     bool
	choiceSecret []byte
}

func (f *fakeDialogs) BackupPlan(p interact.BackupPlan, answer func()) {
	f.plans = append(f.plans, p)
	answer()
}
func (f *fakeDialogs) ConfirmBackupStart(o interact.BackupStartOptions, answer func(interact.BackupStart, error)) {
	f.opts = append(f.opts, o)
	answer(f.start, nil)
}
func (f *fakeDialogs) RestorePlan(interact.RestorePlan, func()) {}
func (f *fakeDialogs) VerifyPlan(interact.VerifyPlan, func())   {}
func (f *fakeDialogs) ConfirmStart(string, func(bool, error))   {}
func (f *fakeDialogs) ChooseUnlockMethod(_ Question, _ string, answer func(bool, []byte, error)) {
	answer(f.recovery, f.choiceSecret, nil)
}
func (f *fakeDialogs) RecoveryCode(string, func(bool))   {}
func (f *fakeDialogs) SpareYubiKey(Question, func(bool)) {}
func (f *fakeDialogs) Password(q Question, answer func([]byte, error)) {
	f.questions = append(f.questions, q)
	answer(f.password(q))
}
func (f *fakeDialogs) NewPassword(q Question, _ string, answer func([]byte, []byte, bool)) {
	f.questions = append(f.questions, q)
	pw, confirm, ok := f.newPw()
	f.confirm = confirm
	answer(pw, confirm, ok)
}

// newTestUI returns a UI whose questions a goroutine shows like the UI
// thread does.
func newTestUI(t *testing.T, d Dialogs) *UI {
	t.Helper()
	notes := make(chan int, 100)
	b := NewBridge(func(kind int) { notes <- kind })
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case k := <-notes:
				if k == NoteQuestion {
					b.ShowNext()
				}
			case <-stop:
				return
			}
		}
	}()
	return NewUI(b, d)
}

func TestUIPasswordRetriesShowTheWorkflowMessage(t *testing.T) {
	t.Parallel()
	d := &fakeDialogs{password: func(Question) ([]byte, error) { return []byte("pw"), nil }}
	u := newTestUI(t, d)
	if _, err := u.Password("Enter backup password: "); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(u.Output(), "Wrong password. 2 attempt(s) remaining.")
	if _, err := u.Password("Enter backup password: "); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(u.Output(), "[2026-09-30 09:12:03] INFO  - a log line")
	if _, err := u.Password("Enter recovery code: "); err != nil {
		t.Fatal(err)
	}
	q := d.questions
	if q[0].Retry || q[0].Message != "" {
		t.Fatalf("first question: %+v", q[0])
	}
	if !q[1].Retry || q[1].Message != "Wrong password. 2 attempt(s) remaining." {
		t.Fatalf("retry: %+v", q[1])
	}
	if q[2].Retry || q[2].Message != "" {
		t.Fatalf("another prompt after a log line: %+v", q[2])
	}
}

func TestUINewPasswordChecksTheConfirmation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		pw, confirm string
		ok          bool
		want        error
	}{
		{"secret", "secret", true, nil},
		{"secret", "Secret", true, interact.ErrPasswordMismatch},
		{"", "", true, interact.ErrPasswordEmpty},
		{"secret", "secret", false, interact.ErrCancelled},
	} {
		d := &fakeDialogs{newPw: func() ([]byte, []byte, bool) { return []byte(tc.pw), []byte(tc.confirm), tc.ok }}
		u := newTestUI(t, d)
		pw, err := u.NewPassword("New password: ", "Confirm: ")
		if !errors.Is(err, tc.want) || (err == nil && string(pw) != tc.pw) {
			t.Fatalf("%q/%q: %q, %v; want %v", tc.pw, tc.confirm, pw, err, tc.want)
		}
		if tc.ok {
			for _, b := range d.confirm {
				if b != 0 {
					t.Fatalf("%q/%q: the confirmation must be zeroed", tc.pw, tc.confirm)
				}
			}
		}
	}
}

func TestUIPlansAndStartReachTheDialogs(t *testing.T) {
	t.Parallel()
	d := &fakeDialogs{start: interact.BackupFull}
	u := newTestUI(t, d)
	u.ShowBackupPlan(interact.BackupPlan{BackupDir: "D:/Backups"})
	choice, err := u.ConfirmBackupStart(interact.BackupStartOptions{OfferFull: true})
	if err != nil || choice != interact.BackupFull || len(d.plans) != 1 || d.plans[0].BackupDir != "D:/Backups" || !d.opts[0].OfferFull {
		t.Fatalf("choice %v, err %v, plans %+v, opts %+v", choice, err, d.plans, d.opts)
	}
}

func TestUICancelAnswersQuestions(t *testing.T) {
	t.Parallel()
	u := NewUI(NewBridge(func(int) {}), &fakeDialogs{})
	u.Bridge().Close()
	if choice, err := u.ConfirmBackupStart(interact.BackupStartOptions{}); choice != interact.BackupCancel || err != nil {
		t.Fatalf("start after cancel: %v, %v", choice, err)
	}
	if _, err := u.Password("Enter backup password: "); !errors.Is(err, interact.ErrCancelled) {
		t.Fatalf("password after cancel: %v", err)
	}
	if ok, _ := u.WaitForSpareYubiKey(); ok {
		t.Fatal("spare YubiKey after cancel")
	}
}

func TestMachineLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	var m Machine
	if m.Busy() || m.CancelRequested() != CancelIgnore || m.CloseRequested() != CloseNow {
		t.Fatal("idle: nothing runs, closing closes")
	}
	if !m.Start(OpBackup) || m.Start(OpVerify) {
		t.Fatal("one operation at a time")
	}
	if m.CancelRequested() != CancelNow {
		t.Fatal("while planning, Cancel needs no confirmation: nothing is written")
	}
	m.Confirmed(now)
	if m.Stage() != StageRunning || !m.Current().Started.Equal(now) || m.CancelRequested() != CancelAsk || m.CloseRequested() != CloseAsk {
		t.Fatalf("running: %+v", m.Current())
	}
	m.Cancelling()
	if m.CancelRequested() != CancelIgnore || !m.Busy() {
		t.Fatal("cancelling: a second Cancel does nothing")
	}
	if m.CloseRequested() != CloseAfterCancel {
		t.Fatal("closing while cancelling waits for the worker")
	}
	if !m.Done(End{Err: errors.New("Backup cancelled.")}, now) || m.Stage() != StageFinished || !m.Current().Cancelled {
		t.Fatal("the window closes once the worker has finished")
	}
	if !m.Start(OpBackup) {
		t.Fatal("a finished run does not block the next")
	}
}

func TestMachineClosingWhilePlanningCancels(t *testing.T) {
	t.Parallel()
	var m Machine
	m.Start(OpRestore)
	if m.CloseRequested() != CloseAfterCancel {
		t.Fatal("closing while planning cancels and closes")
	}
	if !m.Done(End{}, time.Now()) {
		t.Fatal("the window closes when the worker is done")
	}
	m.Dismiss()
	if m.Current() != nil {
		t.Fatal("dismiss ends the run")
	}
}

func TestMachineProgressResetsTheSpeedPerStep(t *testing.T) {
	t.Parallel()
	var m Machine
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	m.Start(OpBackup)
	m.Progressed(interact.Progress{Done: 5}, now) // ignored while planning
	if m.Current().Progress.Done != 0 {
		t.Fatal("progress before the start is ignored")
	}
	m.Confirmed(now)
	step := interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 2, Item: "Docs", Total: 100}
	for i := range 4 {
		step.Done = int64(i * 10)
		m.Progressed(step, now.Add(time.Duration(i)*time.Second))
	}
	if r := m.Current().Speed.Rate(); r != 10 {
		t.Fatalf("rate %v, want 10 bytes/s", r)
	}
	step.Index, step.Item, step.Done = 2, "Pics", 1
	m.Progressed(step, now.Add(4*time.Second))
	if r := m.Current().Speed.Rate(); r != 0 {
		t.Fatalf("a new folder starts a new rate, got %v", r)
	}
}

func TestSpeedRate(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	var s Speed
	for i := range 21 {
		s.Add(start.Add(time.Duration(i)*time.Second), int64(i)*1_000_000)
	}
	if r := s.Rate(); r != 1_000_000 {
		t.Fatalf("rate %v", r)
	}
	var back Speed
	back.Add(start, 100)
	back.Add(start.Add(time.Second), 50)
	if back.Rate() != 0 {
		t.Fatal("going back starts over")
	}
}

func TestMachineRecordsTheFoldersBackedUp(t *testing.T) {
	t.Parallel()
	var m Machine
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	m.Start(OpBackup)
	m.Confirmed(now)
	m.Progressed(interact.Progress{Phase: interact.PhaseUnlocking}, now)
	m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 2, Item: "Docs", Done: 90, Total: 100}, now)
	m.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 2, Count: 2, Item: "Pics", Done: 7}, now)
	if f := m.Current().Finished; len(f) != 1 || f[0] != (FolderDone{"Docs", 100}) {
		t.Fatalf("after the first folder: %+v", f)
	}
	m.Done(End{Result: &interact.Result{}}, now)
	if f := m.Current().Finished; len(f) != 2 || f[1] != (FolderDone{"Pics", 7}) {
		t.Fatalf("a successful run finishes its last folder: %+v", f)
	}

	var failed Machine
	failed.Start(OpBackup)
	failed.Confirmed(now)
	failed.Progressed(interact.Progress{Phase: interact.PhaseBackingUp, Index: 1, Count: 1, Item: "Docs", Done: 5}, now)
	failed.Done(End{Err: errors.New("disk full")}, now)
	if f := failed.Current().Finished; len(f) != 0 {
		t.Fatalf("a failed folder is not finished: %+v", f)
	}
}

func TestUIPasswordTypedWithTheUnlockChoice(t *testing.T) {
	t.Parallel()
	asked := 0
	d := &fakeDialogs{choiceSecret: []byte("typed"), password: func(Question) ([]byte, error) { asked++; return []byte("again"), nil }}
	u := newTestUI(t, d)
	if recovery, err := u.ChooseUnlockMethod("password only"); recovery || err != nil {
		t.Fatalf("choice %v %v", recovery, err)
	}
	if pw, err := u.Password("Enter verification password: "); string(pw) != "typed" || err != nil || asked != 0 {
		t.Fatalf("the password typed with the choice: %q %v (asked %d)", pw, err, asked)
	}
	// A wrong password asks again, as a retry.
	if pw, _ := u.Password("Enter verification password: "); string(pw) != "again" || !d.questions[0].Retry {
		t.Fatalf("retry %q %+v", pw, d.questions)
	}

	r := &fakeDialogs{recovery: true, password: func(Question) ([]byte, error) { return []byte("CODE"), nil }}
	ur := newTestUI(t, r)
	if recovery, _ := ur.ChooseUnlockMethod("password only"); !recovery {
		t.Fatal("the recovery code was chosen")
	}
	if code, _ := ur.Password("Enter recovery code: "); string(code) != "CODE" || len(r.questions) != 1 {
		t.Fatalf("the recovery code is asked: %q", code)
	}
}
