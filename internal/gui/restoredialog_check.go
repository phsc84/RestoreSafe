package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
)

// startCheck checks the choices after delayMs, or now.
func (w *restoreDialog) startCheck(delayMs uint32) {
	w.plan, w.checkErr, w.planErr = nil, nil, nil
	w.checking = true
	w.checkSeq++
	if delayMs > 0 {
		win32.SetTimer(w.win.hwnd, checkTimerID, delayMs)
		w.update()
		return
	}
	w.runCheck()
}

// runCheck checks the choices on a worker goroutine: the folders and the
// free space may be on a slow drive.
func (w *restoreDialog) runCheck() {
	a := w.a
	if v := view.RestoreViewOf(w.folders, w.checked, w.when, w.dest, nil, nil, false); v.Hint != "" && !v.Checking {
		// Nothing to check yet: no folder checked, an empty or relative path.
		w.checking = false
		w.update()
		return
	}
	seq, dest, sets := w.checkSeq, w.dest, view.Chosen(w.folders, w.checked)
	cfg, backupDir, infos := a.opts.Config, a.backupDir, a.snapshot.Sets
	go func() {
		plan, err := restore.PlanDestination(cfg, backupDir, infos, sets, dest)
		a.mu.Lock()
		a.pendingDest = &destCheck{seq: seq, plan: plan, err: err}
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgDestChecked, 0, 0) //nolint:errcheck
	}()
	w.update()
}

// destCheck is the result of a check of the choices.
type destCheck struct {
	seq  int
	plan interact.RestorePlan
	err  error
}

// destChecked shows a check, unless the choices changed since or the
// restore started.
func (w *restoreDialog) destChecked(c *destCheck) {
	if c == nil || c.seq != w.checkSeq || w.starting {
		return
	}
	w.checking = false
	if c.err != nil {
		w.plan, w.checkErr = nil, c.err
	} else {
		plan := c.plan
		w.plan, w.checkErr = &plan, nil
	}
	w.update()
}

// setPlan shows the workflow's plan, made after Start; it replaces the
// check.
func (w *restoreDialog) setPlan(p interact.RestorePlan) {
	w.checkSeq++ // a check still running is older
	w.plan, w.checkErr, w.checking = &p, nil, false
	w.update()
}
