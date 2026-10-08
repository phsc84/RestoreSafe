package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"time"
)

// Control IDs of the Verify window.
const (
	idVerifyStart = 461 + iota
	idVerifyCancel
	idVerifyDetails
)

const verifyClass = "RestoreSafeVerify"

// verifyDialog is the Verify window (GUI spec figure 7.3), the
// verification's plan, laid out like the backup plan (6.1). It opens when
// the verification starts, with a marquee until the workflow sends its
// plan, and Start answers the workflow's start question; then it closes and
// the progress card on Restore backup takes over (BK-8). It is modal to the
// main window but runs in the main message loop.
type verifyDialog struct {
	a   *app
	win *dialogWindow

	plan *interact.VerifyPlan
	// what names the selection: "today, 09:12".
	what string
	// answer answers ConfirmStart; nil while no question is open.
	answer func(bool, error)
	// ended is set when the verification ended before it started, err why.
	ended bool
	err   error
	view  view.VerifyPlanView
	start win32.HWND
	// table lists the folders, with the splitter that sets its height; it
	// outlives the rebuilds, so the widths the user gives its columns stay.
	table *dialogTable

	// The layout of the last build: the stack and the buttons at the right.
	st      *stack
	buttons []win32.HWND
}

// openVerifyDialog opens the Verify window in its waiting state.
func (a *app) openVerifyDialog(what string) {
	if a.verify != nil {
		return
	}
	win, err := newDialogWindow(a.theme, a.hwnd, verifyClass, view.VerifyTitle)
	if err != nil {
		return
	}
	d := &verifyDialog{a: a, win: win, what: what}
	d.table = newDialogTable(win.theme, win.panel.HWND(), func() { d.layout(false) })
	win.panel.OnNotify = func(hdr *win32.NMHdr) uintptr {
		r, _ := d.table.notify(hdr)
		return r
	}
	win.onDpi = func(widget.Scale) {
		d.table.restyle()
		d.build(false)
	}
	win.onCommand = d.command
	win.defID = func() uint16 {
		if d.start != 0 && win32.IsEnabled(d.start) {
			return idVerifyStart
		}
		return 0
	}
	a.verify = d
	d.build(true)
	win32.Enable(a.hwnd, false)
	win32.ShowWindow(win.hwnd, win32.SW_SHOWNORMAL)
	d.focus()
}

// setPlan shows the workflow's plan; Start waits for its question.
func (d *verifyDialog) setPlan(p interact.VerifyPlan) {
	d.plan = &p
	d.build(false)
}

// ask enables Start; the answer goes to the workflow.
func (d *verifyDialog) ask(answer func(bool, error)) {
	d.answer = answer
	d.build(false)
	d.focus()
}

// build creates the controls for the current state and sizes the window to
// them, as the backup plan does.
func (d *verifyDialog) build(place bool) {
	t := d.win.theme
	s := t.Scale
	pal := t.Palette
	panel := d.win.panel
	panel.Clear()
	d.start = 0
	st := newStack(t, panel, s.Px(planWidth-2*planMargin))
	d.st = st
	d.table.begin(st, d.plan != nil)
	switch {
	case d.plan == nil && d.ended:
		d.view = view.VerifyPlanView{Cancel: view.Button{Text: view.ButtonCancel, Action: view.ActionCancel, Enabled: true}}
		st.para(issueOf(d.err), widget.TextBody, pal.Error, view.GlyphError)
	case d.plan == nil:
		d.view = view.VerifyPlanView{Cancel: view.Button{Text: view.ButtonCancel, Action: view.ActionCancel, Enabled: true}}
		st.para(view.PlanPreparing, widget.TextBody, pal.Text, view.GlyphNone)
		st.gap(8)
		if bar, err := widget.NewProgressBar(panel.HWND()); err == nil {
			panel.Adopt(bar.HWND())
			win32.SetAccessibleName(bar.HWND(), view.PlanPreparing)
			bar.Set(-1)
			st.row(10, cell{hwnd: bar.HWND(), fill: true})
		}
	default:
		d.view = view.VerifyPlanOf(*d.plan, d.what, time.Now())
		if d.ended {
			d.view.Start = nil // only Cancel is left, as in a blocked backup plan (BP-5)
			if d.err != nil && !d.plan.HasErrors() {
				d.view.Issues = append(d.view.Issues, view.IssueLine{Text: issueOf(d.err), Tone: view.ToneError, Glyph: view.GlyphError})
			}
		}
		d.content(st)
	}

	// Start and Cancel at the right, as in the backup plan.
	var buttons []win32.HWND
	if v := d.view; v.Start != nil {
		d.start = panel.PrimaryButton(v.Start.Text, idVerifyStart)
		win32.Enable(d.start, v.Start.Enabled && d.answer != nil)
		buttons = append(buttons, d.start)
	}
	buttons = append(buttons, panel.Button(d.view.Cancel.Text, idVerifyCancel))
	d.buttons = buttons
	d.layout(place)
}

// layout sizes the window to its content, at most the height of the screen,
// and places the controls; place centers it over the main window.
func (d *verifyDialog) layout(place bool) {
	t := d.win.theme
	s := t.Scale
	st := d.st
	margin := s.Px(planMargin)
	buttonsH := s.Px(widget.ButtonHeight)
	w := s.Px(planWidth)
	h := d.table.fit(margin + st.height() + margin + buttonsH + margin)
	d.win.resize(w, h, place)
	st.place(margin, margin)
	buttons := d.buttons
	row := widget.NewArea(s, win32.Rect{Left: margin, Top: h - margin - buttonsH, Right: w - margin, Bottom: h - margin})
	for i := len(buttons) - 1; i >= 0; i-- {
		win32.SetWindowPos(buttons[i], row.RightPx(buttonWidth(t, buttons[i])))
		row.Right(8)
	}
}

// content adds the plan: heading, folders, lines, note, issues.
func (d *verifyDialog) content(st *stack) {
	t := d.win.theme
	pal := t.Palette
	v := d.view
	panel := d.win.panel
	st.row(stackLineHeight, cell{hwnd: panel.Label(v.Heading, widget.TextStrong, pal.Text), fill: true})
	st.gap(10)
	d.table.set(v.Folders)
	d.table.add(st)
	for _, line := range []view.PlanLine{v.Read, v.Unlock} {
		st.labeled(line.Label, line.Text, pal.Text, line.Glyph)
		st.gap(4)
	}
	st.gap(8)
	st.para(v.Note, widget.TextSmall, pal.TextSecondary, view.GlyphInfo)
	for _, issue := range v.Issues {
		st.gap(6)
		st.para(issue.Text, widget.TextBody, toneColor(pal, issue.Tone), issue.Glyph)
	}
	st.gap(8)
	st.row(stackLineHeight, cell{hwnd: panel.Link(v.Details.Text, idVerifyDetails), dip: 120})
}

// focus puts the focus on Start, or on the window while the workflow works.
func (d *verifyDialog) focus() {
	if d.start != 0 && win32.IsEnabled(d.start) {
		win32.SetFocus(d.start)
		return
	}
	win32.SetFocus(d.win.hwnd)
}

func (d *verifyDialog) command(id uint16) {
	a := d.a
	switch id {
	case idVerifyStart:
		if d.answer == nil {
			return
		}
		answer := d.takeAnswer()
		d.close()
		a.runStarted()
		answer(true, nil)
	case idVerifyCancel, win32.IDCANCEL:
		if d.answer != nil {
			answer := d.takeAnswer()
			d.close()
			answer(false, nil)
			return
		}
		d.close()
		a.cancelRun()
	case idVerifyDetails:
		if d.plan != nil {
			a.showDetails(d.win.hwnd, view.VerifyDetailsTitle, d.plan.Details)
		}
	}
}

// workerDone keeps the window open when the verification ended before it
// started (a blocked plan): it shows why, and Cancel closes it. It reports
// whether it took care of the end.
func (d *verifyDialog) workerDone() bool {
	r := d.a.machine.Current()
	if r == nil || !r.Started.IsZero() {
		d.close()
		return false
	}
	d.a.machine.Dismiss()
	d.answer, d.ended, d.err = nil, true, r.Err
	d.build(false)
	d.focus()
	return true
}

// issueOf is a workflow error without "Remedy:".
func issueOf(err error) string {
	if err == nil {
		return ""
	}
	return view.ErrorText(err)
}

// takeAnswer returns the pending answer and forgets it, so it is given once.
func (d *verifyDialog) takeAnswer() func(bool, error) {
	answer := d.answer
	d.answer = nil
	return answer
}

// close closes the window. A pending question is answered by the bridge
// when the operation is cancelled.
func (d *verifyDialog) close() {
	a := d.a
	if a.verify != d {
		return
	}
	a.verify = nil
	win32.Enable(a.hwnd, true)
	d.win.destroy()
	a.focusPage()
}
