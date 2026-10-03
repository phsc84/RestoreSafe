package gui

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"fmt"
	"strings"
	"time"
)

// runState is the worker of the operation in progress; a.machine holds its
// stage.
type runState struct {
	b      *flow.Bridge
	cancel context.CancelFunc
	doneCh chan workerEnd
}

// workerEnd is how the worker ended: the workflow's error and the facts of
// its log.
type workerEnd struct {
	err   error
	facts logging.RunFacts
}

// runFacts reads the facts of the run's log; it runs on the worker, as the
// log may be on a slow network share. Without a log (the run failed before
// it opened one) there are none.
func runFacts(logPath string) logging.RunFacts {
	if logPath == "" {
		return logging.RunFacts{}
	}
	facts, _ := logging.ReadFacts(logPath) //nolint:errcheck // the result card does without
	return facts
}

// opRequest is an operation and what the user chose for it.
type opRequest struct {
	op flow.Op
	// sets are the sets to restore or verify; destination is where to
	// restore them.
	sets        []naming.BackupEntry
	destination string
}

// startOperation runs req in a worker goroutine. A backup opens its plan
// dialog and shows its progress on Create backup; a verification shows its
// progress on Restore backup; a restore runs in the restore wizard, which
// started it, and shows its progress on Restore backup too.
func (a *app) startOperation(req opRequest) {
	op := req.op
	if !a.machine.Start(op) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, doneCh: make(chan workerEnd, 1)}
	r.b = flow.NewBridge(func(kind int) {
		win32.PostMessage(a.hwnd, msgBridge, uintptr(kind), 0) //nolint:errcheck
	})
	a.run = r
	a.logText.Reset()
	u := flow.NewUI(r.b, questions{a})

	switch op {
	case flow.OpBackup:
		a.showPage(view.PageOverview)
		a.refreshRun()
		a.openPlanDialog()
	case flow.OpVerify:
		a.showPage(view.PageBackups)
		a.refreshRun()
	}
	a.updateTaskbar()

	cfg, exeDir := a.opts.Config, a.opts.ExeDir
	go func() {
		var err error
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("Internal error: %v", p)
			}
			r.doneCh <- workerEnd{err, runFacts(r.b.LogPath())}
			win32.PostMessage(a.hwnd, msgWorkerDone, 0, 0) //nolint:errcheck
		}()
		switch op {
		case flow.OpBackup:
			err = backup.Run(ctx, u, cfg, exeDir)
		case flow.OpRestore:
			err = restore.Run(ctx, u, cfg, exeDir, restore.Request{Sets: req.sets, Destination: req.destination})
		case flow.OpVerify:
			err = verify.Run(ctx, u, cfg, exeDir, verify.Request{Sets: req.sets})
		}
	}()
}

// runStarted records that the user started the backup in the plan dialog.
func (a *app) runStarted() {
	a.machine.Confirmed(time.Now())
	a.refreshRun()
	a.focusPage()
}

// refreshRun shows the state of the operation on its page, and on the
// taskbar button.
func (a *app) refreshRun() {
	a.shell.overview.updateRun()
	if a.wizard != nil {
		a.wizard.update()
	}
	a.shell.backups.updateRun()
	a.updateTaskbar()
}

// onProgress shows the latest progress report.
func (a *app) onProgress() {
	if a.run == nil {
		return
	}
	p := a.run.b.TakeProgress()
	a.machine.Progressed(p, time.Now())
	if a.machine.Stage() != flow.StageRunning {
		return
	}
	a.refreshRun()
}

// onOutput keeps new output for the log pane.
func (a *app) onOutput() {
	if a.run == nil {
		return
	}
	text := a.run.b.TakeOutput()
	if text != "" {
		a.logText.WriteString(text)
		a.shell.backups.appendLive(text)
	}
}

// confirmCancel handles Cancel while an operation runs: it asks first once
// the operation has started (figure 6.3).
func (a *app) confirmCancel() {
	switch a.machine.CancelRequested() {
	case flow.CancelNow:
		a.cancelRun()
	case flow.CancelAsk:
		if a.confirm(a.dialogOwner(), view.CancelConfirm(a.machine.Current().Op, false)) {
			a.cancelRun()
		}
	}
}

// cancelRun cancels the operation: the context stops the running work, and
// the bridge answers pending and later questions with their cancel answers.
func (a *app) cancelRun() {
	r := a.run
	if r == nil || a.machine.Stage() == flow.StageCancelling {
		return
	}
	a.machine.Cancelling()
	r.cancel()
	r.b.Close()
	a.closeModal()
	if a.plan != nil {
		a.plan.close()
	}
	a.refreshRun()
}

// onWorkerDone shows the result, or closes the window when that was
// requested while the operation ran.
func (a *app) onWorkerDone() {
	r := a.run
	if r == nil {
		return
	}
	end := <-r.doneCh
	r.cancel()
	a.onOutput()
	a.shell.backups.reloadLog()
	a.run = nil
	if a.plan != nil {
		// The plan blocked the start: the result card says why.
		a.plan.close()
	}
	if a.machine.Done(flow.End{Result: r.b.FinalResult(), Err: end.err, Facts: end.facts, LogPath: r.b.LogPath()}, time.Now()) {
		win32.UnblockShutdown(a.hwnd)
		win32.DestroyWindow(a.hwnd)
		return
	}
	if cfg := a.deferredConfig; cfg != nil && !a.machine.Busy() {
		a.useConfig(cfg)
	}
	if run := a.machine.Current(); run.Op == flow.OpRestore && a.wizard != nil && a.wizard.workerDone() {
		// The restore ended before it started; the wizard shows why.
		a.refreshRun()
		return
	}
	a.startCheck()
	a.refreshRun()
	if a.wizard == nil {
		a.focusPage()
	}
	a.updateTaskbar()
	if !win32.IsForeground(a.hwnd) {
		win32.FlashUntilActive(a.hwnd)
	}
}

// dismiss ends the shown result, and closes the restore wizard that shows
// it: the Overview shows the state again.
func (a *app) dismiss() {
	a.machine.Dismiss()
	if a.wizard != nil {
		a.wizard.close()
	}
	a.refreshShell()
	a.updateTaskbar()
	a.focusPage()
}

// showRunLog shows the log of the operation on the Backups page (spec
// BR-7): the run selected, its log in the pane, live while it runs.
func (a *app) showRunLog() {
	path := ""
	if a.run != nil {
		path = a.run.b.LogPath()
	} else if r := a.machine.Current(); r != nil {
		path = r.LogPath
	}
	if path == "" {
		return
	}
	a.showPage(view.PageBackups)
	a.shell.backups.showLogOf(path)
	a.focusPage()
}

// showResultDetails shows the workflow's message of the result card: the
// plan's preflight when the plan blocked the start.
func (a *app) showResultDetails() {
	r := a.machine.Current()
	if r == nil {
		return
	}
	if r.Started.IsZero() && r.Plan != nil {
		a.showDetails(a.hwnd, view.PlanDetailsTitle, r.Plan.Details)
		return
	}
	if c := view.ResultCardOf(r); c != nil {
		a.showText(a.hwnd, view.DetailsOfResult, c.Detail)
	}
}

// onClose handles closing the window (spec 6.4, 12.4).
func (a *app) onClose() {
	switch a.machine.CloseRequested() {
	case flow.CloseNow:
		win32.DestroyWindow(a.hwnd)
	case flow.CloseAfterCancel:
		a.cancelRun()
	case flow.CloseAsk:
		if a.confirm(a.hwnd, view.CancelConfirm(a.machine.Current().Op, true)) {
			a.machine.CloseConfirmed()
			a.cancelRun()
		}
	}
}

// onQueryEndSession cancels a running operation when Windows ends the
// session and asks Windows to wait until it has cleaned up. It reports
// whether the session may end now.
func (a *app) onQueryEndSession() bool {
	if a.run == nil {
		return true
	}
	a.machine.CloseConfirmed()
	a.cancelRun()
	win32.BlockShutdown(a.hwnd, fmt.Sprintf("RestoreSafe is stopping the %s and cleaning up.", strings.ToLower(opName(a.machine.Current().Op))))
	return false
}

// onEndSession waits (bounded) for the worker when the session ends anyway.
func (a *app) onEndSession() {
	r := a.run
	if r == nil {
		return
	}
	a.cancelRun()
	select {
	case end := <-r.doneCh:
		r.doneCh <- end
	case <-time.After(20 * time.Second):
	}
}

// updateTaskbar shows the operation on the taskbar button (spec BR-5):
// progress while it runs, red after a failure, amber after warnings.
func (a *app) updateTaskbar() {
	tb := a.taskbar
	if tb == nil {
		return
	}
	r := a.machine.Current()
	switch {
	case r == nil || r.Stage == flow.StagePlanning:
		tb.SetState(win32.TaskbarNoProgress)
	case r.Stage == flow.StageCancelling:
		tb.SetState(win32.TaskbarPaused)
	case r.Stage == flow.StageRunning:
		if f := r.Progress.Fraction(); f >= 0 && r.Progress.Phase != interact.PhaseUnlocking {
			tb.SetState(win32.TaskbarNormal)
			tb.SetValue(uint64(f*1000), 1000)
		} else {
			tb.SetState(win32.TaskbarIndeterminate)
		}
	default:
		c := view.ResultCardOf(r)
		switch {
		case c == nil || c.Tone == view.ToneSuccess || c.Tone == view.ToneNeutral:
			tb.SetState(win32.TaskbarNoProgress)
		case c.Tone == view.ToneError:
			tb.SetState(win32.TaskbarError)
			tb.SetValue(1000, 1000)
		default:
			tb.SetState(win32.TaskbarPaused)
			tb.SetValue(1000, 1000)
		}
	}
}

// opName returns the operation's name as the workflows use it in messages.
func opName(o flow.Op) string {
	switch o {
	case flow.OpRestore:
		return "Restore"
	case flow.OpVerify:
		return "Verification"
	}
	return "Backup"
}
