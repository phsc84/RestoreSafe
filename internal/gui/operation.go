package gui

import (
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
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

// runState is the operation in progress.
type runState struct {
	op      operation
	b       *bridge
	cancel  context.CancelFunc
	doneCh  chan error
	started time.Time // when the running screen appeared; zero before

	cancelling    bool
	closeWhenDone bool
	report        *interact.Report // last preflight report
}

// Operation screen timer.
const elapsedTimerID = 1

// startOperation runs op in a worker goroutine and shows the operation
// screen.
func (a *app) startOperation(op operation) {
	if a.run != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{op: op, cancel: cancel, doneCh: make(chan error, 1)}
	r.b = newBridge(func(kind int) {
		win32.PostMessage(a.hwnd, msgBridge, uintptr(kind), 0) //nolint:errcheck
	})
	a.run = r
	g := &guiUI{app: a, b: r.b, op: op}

	a.showPage(pageOperation)
	win32.SetText(a.hwnd, "RestoreSafe "+a.opts.Version+" - "+op.title())
	win32.SetRichText(a.op.log, "")
	a.opReport = nil
	a.setOpScreen(op.title(), interact.StatusNone, "Preparing ...", contentLog, false, nil)

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
		case opBackup:
			err = backup.Run(ctx, g, cfg, exeDir)
		case opRestore:
			err = restore.Run(ctx, g, cfg, exeDir)
		case opVerify:
			err = verify.Run(ctx, g, cfg, exeDir)
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
	a.setOpScreen(a.run.op.title(), interact.StatusNone, detail, contentReport, false, nil)
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
	r := a.run
	if r == nil {
		return
	}
	r.started = time.Now()
	a.setOpScreen(r.op.name(), interact.StatusNone, "Unlocking keys ...", contentLog, true, []opButton{{"Cancel", a.confirmCancel}})
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
	r := a.run
	if r == nil || r.started.IsZero() || r.cancelling {
		return
	}
	p := r.b.takeProgress()
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
	r := a.run
	if r == nil || r.started.IsZero() || r.cancelling {
		return
	}
	elapsed := time.Since(r.started).Truncate(time.Second)
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
	text := a.run.b.takeOutput()
	if text != "" {
		win32.AppendText(a.op.log, strings.ReplaceAll(text, "\n", "\r\n"))
	}
}

// confirmCancel asks before cancelling a running operation.
func (a *app) confirmCancel() {
	r := a.run
	if r == nil || r.cancelling {
		return
	}
	if !a.confirmCancelDialog() {
		return
	}
	a.cancelRun()
}

func (a *app) confirmCancelDialog() bool {
	content := map[operation]string{
		opBackup:  "Backup sets completed so far are kept; the one being written is removed.",
		opRestore: "Directories restored so far are kept; the one being restored will be incomplete.",
		opVerify:  "Cancelling a verification changes nothing.",
	}[a.run.op]
	button, _ := a.taskDialog(win32.TaskDialog{
		Instruction: fmt.Sprintf("Cancel the running %s?", strings.ToLower(a.run.op.name())),
		Content:     content,
		Icon:        win32.TD_WARNING_ICON,
		Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: "Cancel " + strings.ToLower(a.run.op.name())}, {ID: win32.IDCANCEL, Text: "Continue"}},
		Default:     win32.IDCANCEL,
	})
	return button == win32.IDOK
}

// cancelRun cancels the operation: the context stops the running work, and
// the bridge answers pending and later questions with their cancel answers.
func (a *app) cancelRun() {
	r := a.run
	if r == nil || r.cancelling {
		return
	}
	r.cancelling = true
	r.cancel()
	r.b.close()
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
	if r.closeWhenDone {
		win32.UnblockShutdown(a.hwnd)
		win32.DestroyWindow(a.hwnd)
		return
	}

	res := r.b.finalResult()
	o := operationOutcome(r.op, res, err)
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

// backToHome returns to the home screen and runs the health check again, as
// the backup directory has changed.
func (a *app) backToHome() {
	win32.SetText(a.hwnd, "RestoreSafe "+a.opts.Version)
	a.showPage(pageHome)
	a.startHealthCheck()
	a.focusHome()
}

// onClose handles closing the window (docs/SPEC-restoresafe-gui.md, 7.3).
func (a *app) onClose() {
	r := a.run
	switch {
	case r == nil:
		win32.DestroyWindow(a.hwnd)
	case r.cancelling:
		r.closeWhenDone = true
	case r.started.IsZero():
		// Only questions so far: nothing is written yet.
		r.closeWhenDone = true
		a.cancelRun()
	default:
		if a.confirmCancelDialog() {
			r.closeWhenDone = true
			a.cancelRun()
		}
	}
}

// onQueryEndSession cancels a running operation when Windows ends the
// session and asks Windows to wait until it has cleaned up. It reports
// whether the session may end now.
func (a *app) onQueryEndSession() bool {
	r := a.run
	if r == nil {
		return true
	}
	r.closeWhenDone = true
	a.cancelRun()
	win32.BlockShutdown(a.hwnd, fmt.Sprintf("RestoreSafe is stopping the %s and cleaning up.", strings.ToLower(r.op.name())))
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
