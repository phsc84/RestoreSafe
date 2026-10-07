package flow

import (
	"RestoreSafe/internal/logging"
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
	// Plan is the last backup plan shown; Verify and Restore the plans of
	// a verification and a restore.
	Plan    *interact.BackupPlan
	Verify  *interact.VerifyPlan
	Restore *interact.RestorePlan
	// What names the backup a verification reads: "today, 09:12"; Whole is
	// set when it reads every folder of it.
	What  string
	Whole bool
	// Progress is the latest report, Speed the rate of its folder.
	Progress interact.Progress
	Speed    Speed
	// Finished are the folders backed up or restored so far, in order.
	Finished []FolderDone
	// CloseWhenDone closes the window once the worker has finished.
	CloseWhenDone bool
	// Result and Err are the outcome, Facts what the run's log recorded,
	// LogPath its log file, Ended when the worker finished.
	Result  *interact.Result
	Err     error
	Facts   logging.RunFacts
	LogPath string
	Ended   time.Time
	// Cancelled is set when the user cancelled.
	Cancelled bool
}

// Machine is the lifecycle of the operations: at most one runs at a time
// (GUI spec 3.2), and its stage decides what Cancel and closing the window do
// (GUI spec 6.2 and 6.4). It holds no window.
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
		r.finishStep()
		r.Speed = Speed{}
	}
	r.Progress = p
	r.Speed.Add(now, p.Done)
}

// finishStep records the folder of the current step as backed up or
// restored.
func (r *Run) finishStep() {
	p := r.Progress
	if (p.Phase != interact.PhaseBackingUp && p.Phase != interact.PhaseRestoring) || p.Item == "" {
		return
	}
	bytes := p.Done
	switch {
	case p.Written > 0:
		bytes = p.Written
	case p.Total > 0:
		bytes = p.Total
	}
	r.Finished = append(r.Finished, FolderDone{Name: p.Item, Bytes: bytes})
}

// FolderDone is a folder a backup has backed up or a restore restored.
type FolderDone struct {
	Name string
	// Bytes is the size of the backup set a backup wrote, or of the folder
	// a restore read.
	Bytes int64
}

// CancelAction is what a click on Cancel does.
type CancelAction int

const (
	// CancelIgnore: nothing to cancel, or already cancelling.
	CancelIgnore CancelAction = iota
	// CancelNow: cancel without asking; nothing is written yet.
	CancelNow
	// CancelAsk: ask the user first (GUI spec BR-6).
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
	// CloseAsk: ask whether to cancel and close (GUI spec 6.4).
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

// End is how the worker ended.
type End struct {
	// Result is what the workflow reported with ShowResult, nil without.
	Result *interact.Result
	// Err is the workflow's error.
	Err error
	// Facts are what the run's log recorded; LogPath is the log file, ""
	// when the workflow opened none.
	Facts   logging.RunFacts
	LogPath string
}

// Done records the end of the worker. It returns true when the window
// should close now.
func (m *Machine) Done(end End, now time.Time) (closeWindow bool) {
	r := m.run
	if r == nil {
		return false
	}
	if end.Err == nil && end.Result != nil {
		r.finishStep()
	}
	r.Result, r.Err, r.Facts, r.LogPath, r.Ended = end.Result, end.Err, end.Facts, end.LogPath, now
	r.Stage = StageFinished
	return r.CloseWhenDone
}

// Dismiss ends a finished run.
func (m *Machine) Dismiss() {
	if m.Stage() == StageFinished {
		m.run = nil
	}
}

// VerifyPlanShown records the verification plan on screen.
func (m *Machine) VerifyPlanShown(p interact.VerifyPlan) {
	if m.run != nil {
		m.run.Verify = &p
	}
}

// RestorePlanShown records the restore plan on screen.
func (m *Machine) RestorePlanShown(p interact.RestorePlan) {
	if m.run != nil {
		m.run.Restore = &p
	}
}
