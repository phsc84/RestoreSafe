package gui

import (
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"context"
	"fmt"
	"strings"
	"time"
)

// opButton is a button of the operation screen.
type opButton struct {
	text    string
	onClick func()
}

// runState is the worker of the operation in progress; a.machine holds its
// stage.
type runState struct {
	b      *flow.Bridge
	cancel context.CancelFunc
	doneCh chan error
	report *interact.Report // last preflight report
}

// Operation screen timer.
const elapsedTimerID = 1

// startOperation runs op in a worker goroutine and shows the operation
// screen.
func (a *app) startOperation(op flow.Op) {
	if !a.machine.Start(op) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, doneCh: make(chan error, 1)}
	r.b = flow.NewBridge(func(kind int) {
		win32.PostMessage(a.hwnd, msgBridge, uintptr(kind), 0) //nolint:errcheck
	})
	a.run = r
	u := flow.NewUI(r.b, questions{a})

	a.showPage(pageOperation)
	win32.SetText(a.hwnd, "RestoreSafe "+a.opts.Version+" - "+opTitle(op))
	win32.SetRichText(a.op.log, "")
	a.opReport = nil
	a.setOpScreen(opTitle(op), interact.StatusNone, "Preparing ...", contentLog, false, nil)

	cfg, exeDir := a.opts.Config, a.opts.ExeDir
	go func() {
		var err error
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("Internal error: %v", p)
			}
			r.doneCh <- err
			win32.PostMessage(a.hwnd, msgWorkerDone, 0, 0) //nolint:errcheck
		}()
		switch op {
		case flow.OpBackup:
			err = backup.Run(ctx, u, cfg, exeDir)
		case flow.OpRestore:
			err = a.runRestore(ctx, u, cfg, exeDir)
		case flow.OpVerify:
			err = a.runVerify(ctx, u, cfg, exeDir)
		}
	}()
}

// setOpScreen sets the operation screen's heading, detail line, content
// area, progress bar, and buttons (nil: none).
func (a *app) setOpScreen(title string, status interact.Status, detail string, content opContent, showProgress bool, buttons []opButton) {
	a.opTitleStatus = status
	win32.SetText(a.op.title, title)
	win32.SetText(a.op.detail, detail)
	a.opContent = content
	a.opShowProgress = showProgress
	a.opButtons = nil
	a.applyOpVisibility()
	a.setOpButtons(buttons)
	a.layout()
}

// setOpButtons shows the given buttons from the left and hides the rest.
func (a *app) setOpButtons(buttons []opButton) {
	a.opButtons = buttons
	for i, h := range a.op.buttons {
		if i < len(buttons) {
			win32.SetText(h, buttons[i].text)
			win32.Enable(h, true)
			win32.SetVisible(h, true)
		} else {
			setShown(h, false)
		}
	}
	if len(buttons) > 0 {
		win32.SetFocus(a.op.buttons[0])
	}
}

// showPreflight shows the preflight report.
func (a *app) showPreflight(r interact.Report) {
	if a.run == nil {
		return
	}
	a.run.report = &r
	a.opReport = &r
	win32.SetRichText(a.op.report, reportRTF(r, a.fontFace, a.fontPt))
	detail := "Check the summary, then start."
	if r.HasErrors() {
		detail = "The preflight found errors."
	}
	a.setOpScreen(opTitle(a.machine.Current().Op), interact.StatusNone, detail, contentReport, false, nil)
}

// offerStart shows the start buttons under the preflight.
func (a *app) offerStart(buttons []opButton) {
	if a.run == nil {
		return
	}
	a.setOpButtons(buttons)
}

// startRunning switches to the running screen after the start was
// confirmed.
func (a *app) startRunning() {
	if a.run == nil {
		return
	}
	a.machine.Confirmed(time.Now())
	a.setOpScreen(opName(a.machine.Current().Op), interact.StatusNone, "Unlocking keys ...", contentLog, true, []opButton{{"Cancel", a.confirmCancel}})
	a.setMarquee(true)
	win32.SetTimer(a.hwnd, elapsedTimerID, 1000)
}

// setMarquee switches the progress bar between an moving marquee (unknown
// progress) and a normal bar.
func (a *app) setMarquee(on bool) {
	style := win32.Style(a.op.progress)
	if on == (style&win32.PBS_MARQUEE != 0) {
		return
	}
	if on {
		win32.SetStyle(a.op.progress, style|win32.PBS_MARQUEE)
		win32.SendMessage(a.op.progress, win32.PBM_SETMARQUEE, 1, 30)
	} else {
		win32.SendMessage(a.op.progress, win32.PBM_SETMARQUEE, 0, 0)
		win32.SetStyle(a.op.progress, style&^win32.PBS_MARQUEE)
		win32.SendMessage(a.op.progress, win32.PBM_SETRANGE32, 0, 1000)
	}
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
	title := p.Step
	if p.Item != "" {
		title += " - " + p.Item
	}
	win32.SetText(a.op.title, title)
	f := p.Fraction()
	a.setMarquee(f < 0)
	if f >= 0 {
		win32.SendMessage(a.op.progress, win32.PBM_SETPOS, uintptr(f*1000), 0)
	}
	a.progressText = progressText(p)
	a.updateElapsed()
}

// progressText describes p's bytes, e.g. "1.2 GiB of 3.4 GiB (35 %)".
func progressText(p interact.Progress) string {
	f := p.Fraction()
	if f < 0 {
		return fsx.FormatBytesBinary(uint64(max(p.Done, 0)))
	}
	return fmt.Sprintf("%s of %s (%d %%)", fsx.FormatBytesBinary(uint64(max(p.Done, 0))), fsx.FormatBytesBinary(uint64(p.Total)), int(f*100))
}

// updateElapsed refreshes the detail line with the progress and the elapsed
// time.
func (a *app) updateElapsed() {
	if a.machine.Stage() != flow.StageRunning {
		return
	}
	elapsed := time.Since(a.machine.Current().Started).Truncate(time.Second)
	text := a.progressText
	if text == "" {
		text = "Unlocking keys ..."
	}
	win32.SetText(a.op.detail, fmt.Sprintf("%s  ·  %s elapsed", text, formatElapsed(elapsed)))
}

func formatElapsed(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

// onOutput appends new output to the log pane.
func (a *app) onOutput() {
	if a.run == nil {
		return
	}
	text := a.run.b.TakeOutput()
	if text != "" {
		win32.AppendText(a.op.log, strings.ReplaceAll(text, "\n", "\r\n"))
	}
}

// confirmCancel handles Cancel while an operation runs: it asks first once
// the operation has started.
func (a *app) confirmCancel() {
	switch a.machine.CancelRequested() {
	case flow.CancelNow:
		a.cancelRun()
	case flow.CancelAsk:
		if a.confirmCancelDialog() {
			a.cancelRun()
		}
	}
}

func (a *app) confirmCancelDialog() bool {
	op := a.machine.Current().Op
	content := map[flow.Op]string{
		flow.OpBackup:  "Backup sets completed so far are kept; the one being written is removed.",
		flow.OpRestore: "Directories restored so far are kept; the one being restored will be incomplete.",
		flow.OpVerify:  "Cancelling a verification changes nothing.",
	}[op]
	name := strings.ToLower(opName(op))
	button, _ := a.taskDialog(win32.TaskDialog{
		Instruction: fmt.Sprintf("Cancel the running %s?", name),
		Content:     content,
		Icon:        win32.TD_WARNING_ICON,
		Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: "Cancel " + name}, {ID: win32.IDCANCEL, Text: "Continue"}},
		Default:     win32.IDCANCEL,
	})
	return button == win32.IDOK
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
	win32.SetText(a.op.detail, "Cancelling ... RestoreSafe finishes the current step and cleans up.")
	a.setMarquee(true)
	for _, h := range a.op.buttons {
		win32.Enable(h, false)
	}
}

// onWorkerDone shows the result screen, or closes the window when that was
// requested while the operation ran.
func (a *app) onWorkerDone() {
	r := a.run
	if r == nil {
		return
	}
	err := <-r.doneCh
	r.cancel()
	a.onOutput()
	win32.KillTimer(a.hwnd, elapsedTimerID)
	a.run = nil
	a.progressText = ""
	res := r.b.FinalResult()
	if a.machine.Done(res, err, time.Now()) {
		win32.UnblockShutdown(a.hwnd)
		win32.DestroyWindow(a.hwnd)
		return
	}

	o := operationOutcome(a.machine.Current().Op, res, err)
	detail := ""
	var buttons []opButton
	if res != nil && res.LogPath != "" {
		detail = "Log file: " + res.LogPath
		logPath := res.LogPath
		buttons = append(buttons, opButton{"Open &log", func() { a.open(logPath, true) }})
	}
	buttons = append(buttons, opButton{"&Back to start", a.backToHome})
	content := contentLog
	if o.showReport && r.report != nil {
		content = contentReportAndLog
	}
	a.setOpScreen(statusPrefix(o.status)+o.text, o.status, detail, content, false, buttons)
	win32.SetFocus(a.op.buttons[len(buttons)-1])
}

// statusPrefix returns the marker shown before a result line.
func statusPrefix(s interact.Status) string {
	if m, _ := statusMarker(s); m != "" {
		return m + "  "
	}
	return ""
}

// backToHome returns to the Overview and checks the backups again, as
// the backup directory has changed.
func (a *app) backToHome() {
	a.machine.Dismiss()
	a.showPage(view.PageOverview)
	a.startCheck()
	a.focusPage()
}

// onClose handles closing the window (spec 6.4).
func (a *app) onClose() {
	switch a.machine.CloseRequested() {
	case flow.CloseNow:
		win32.DestroyWindow(a.hwnd)
	case flow.CloseAfterCancel:
		a.cancelRun()
	case flow.CloseAsk:
		if a.confirmCancelDialog() {
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
	case err := <-r.doneCh:
		r.doneCh <- err
	case <-time.After(20 * time.Second):
	}
}
