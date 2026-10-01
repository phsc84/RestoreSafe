package flow

import (
	"RestoreSafe/internal/workflow/interact"
	"time"
)

// Op is the operation a run performs.
type Op int

const (
	OpBackup Op = iota
	OpRestore
	OpVerify
)

// Stage is where a run is in its lifecycle (spec section 4).
type Stage int

const (
	// StageIdle: no operation runs.
	StageIdle Stage = iota
	// StagePlanning: the workflow plans and asks; nothing is written yet.
	StagePlanning
	// StageRunning: the user started it; it unlocks the keys and works.
	StageRunning
	// StageCancelling: cancel was requested; the worker cleans up.
	StageCancelling
	// StageFinished: the result is shown until the user dismisses it.
	StageFinished
)

// Run is the state of the current operation.
type Run struct {
	Op    Op
	Stage Stage
	// Started is when the user started it; zero while planning.
	Started time.Time
	// Plan is the last backup plan shown.
	Plan *interact.BackupPlan
	// Progress is the latest report, Speed its rate.
	Progress interact.Progress
	Speed    Speed
	// CloseWhenDone closes the window once the worker has finished.
	CloseWhenDone bool
	// Result and Err are the outcome, Ended when the worker finished.
	Result *interact.Result
	Err    error
	Ended  time.Time
	// Cancelled is set when the user cancelled.
	Cancelled bool
}

// Machine is the lifecycle of the operations: at most one runs at a time
// (spec 3.2), and its stage decides what Cancel and closing the window do
// (spec 6.2 and 6.4). It holds no window.
type Machine struct {
	run *Run
}

// Current returns the current run, or nil when idle.
func (m *Machine) Current() *Run { return m.run }

// Stage returns the stage of the current run.
func (m *Machine) Stage() Stage {
	if m.run == nil {
		return StageIdle
	}
	return m.run.Stage
}

// Busy reports whether an operation runs (planning, running, cancelling):
// another one cannot start.
func (m *Machine) Busy() bool {
	s := m.Stage()
	return s == StagePlanning || s == StageRunning || s == StageCancelling
}

// Start begins op. It returns false while another operation is busy; a
// finished one is dismissed.
func (m *Machine) Start(op Op) bool {
	if m.Busy() {
		return false
	}
	m.run = &Run{Op: op, Stage: StagePlanning}
	return true
}

// PlanShown records the backup plan on screen.
func (m *Machine) PlanShown(p interact.BackupPlan) {
	if m.run != nil {
		m.run.Plan = &p
	}
}

// Confirmed records that the user started the operation.
func (m *Machine) Confirmed(now time.Time) {
	if m.run != nil && m.run.Stage == StagePlanning {
		m.run.Stage = StageRunning
		m.run.Started = now
	}
}

// Progressed records a progress report.
func (m *Machine) Progressed(p interact.Progress, now time.Time) {
	r := m.run
	if r == nil || r.Stage != StageRunning {
		return
	}
	if p.Phase != r.Progress.Phase || p.Index != r.Progress.Index || p.Item != r.Progress.Item {
		r.Speed = Speed{}
	}
	r.Progress = p
	r.Speed.Add(now, p.Done)
}

// CancelAction is what a click on Cancel does.
type CancelAction int

const (
	// CancelIgnore: nothing to cancel, or already cancelling.
	CancelIgnore CancelAction = iota
	// CancelNow: cancel without asking; nothing is written yet.
	CancelNow
	// CancelAsk: ask the user first (spec BR-6).
	CancelAsk
)

// CancelRequested tells what Cancel does in the current stage.
func (m *Machine) CancelRequested() CancelAction {
	switch m.Stage() {
	case StagePlanning:
		return CancelNow
	case StageRunning:
		return CancelAsk
	}
	return CancelIgnore
}

// Cancelling records that the operation is being cancelled.
func (m *Machine) Cancelling() {
	if r := m.run; r != nil && (r.Stage == StagePlanning || r.Stage == StageRunning) {
		r.Stage = StageCancelling
		r.Cancelled = true
	}
}

// CloseAction is what closing the window does.
type CloseAction int

const (
	// CloseNow: close the window.
	CloseNow CloseAction = iota
	// CloseAsk: ask whether to cancel and close (spec 6.4).
	CloseAsk
	// CloseAfterCancel: cancel (if not already) and close once the worker
	// has finished; the caller cancels when the stage is not yet
	// StageCancelling.
	CloseAfterCancel
)

// CloseRequested tells what closing the window does in the current stage.
func (m *Machine) CloseRequested() CloseAction {
	switch m.Stage() {
	case StagePlanning, StageCancelling:
		m.run.CloseWhenDone = true
		return CloseAfterCancel
	case StageRunning:
		return CloseAsk
	}
	return CloseNow
}

// CloseConfirmed records that the user chose to cancel and close.
func (m *Machine) CloseConfirmed() {
	if m.run != nil {
		m.run.CloseWhenDone = true
	}
}

// Done records the end of the worker. It returns true when the window
// should close now.
func (m *Machine) Done(res *interact.Result, err error, now time.Time) (closeWindow bool) {
	r := m.run
	if r == nil {
		return false
	}
	r.Result, r.Err, r.Ended = res, err, now
	r.Stage = StageFinished
	return r.CloseWhenDone
}

// Dismiss ends a finished run.
func (m *Machine) Dismiss() {
	if m.Stage() == StageFinished {
		m.run = nil
	}
}
