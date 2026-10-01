package gui

import (
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"time"
)

// The first GUI's operation screen, which restore and verify use until the
// restore wizard and the Backups page replace it (plan phases 7 and 8).

// opButton is a button of the operation screen.
type opButton struct {
	text    string
	onClick func()
}

// Operation screen timer.
const elapsedTimerID = 1

// showOpScreen switches to the operation screen for op.
func (a *app) showOpScreen(op flow.Op) {
	a.showPage(pageOperation)
	win32.SetText(a.hwnd, "RestoreSafe "+a.opts.Version+" - "+opTitle(op))
	win32.SetRichText(a.op.log, "")
	a.opReport = nil
	a.setOpScreen(opTitle(op), interact.StatusNone, "Preparing ...", contentLog, false, nil)
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
	a.updateTaskbar()
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

// opScreenProgress shows the progress report p on the operation screen.
func (a *app) opScreenProgress(p interact.Progress) {
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
	if a.machine.Stage() != flow.StageRunning || a.page != pageOperation {
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

// opScreenCancelling shows that the operation is being cancelled.
func (a *app) opScreenCancelling() {
	win32.SetText(a.op.detail, "Cancelling ... RestoreSafe finishes the current step and cleans up.")
	a.setMarquee(true)
	for _, h := range a.op.buttons {
		win32.Enable(h, false)
	}
}

// opScreenResult shows the result of the finished run r.
func (a *app) opScreenResult(r *flow.Run, report *interact.Report) {
	o := operationOutcome(r.Op, r.Result, r.Err)
	detail := ""
	var buttons []opButton
	if res := r.Result; res != nil && res.LogPath != "" {
		detail = "Log file: " + res.LogPath
		logPath := res.LogPath
		buttons = append(buttons, opButton{"Open &log", func() { a.open(logPath, true) }})
	}
	buttons = append(buttons, opButton{"&Back to start", a.dismiss})
	content := contentLog
	if o.showReport && report != nil {
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
