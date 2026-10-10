package gui

import (
	"time"

	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// Control IDs of the plan dialog.
const (
	idPlanStart = 451 + iota
	idPlanFull
	idPlanNewKeys
	idPlanCancel
	idPlanDetails
	idPlanRemoves
)

// Sizes of the plan dialog, in DIPs.
const (
	planWidth  = 680
	planMargin = 18
)

const planClass = "RestoreSafePlan"

// planDialog is the backup plan (GUI spec 6.1). It opens when the backup
// starts, with a marquee while the workflow measures the folders, shows
// each plan the workflow sends, and stays open while the user switches
// plans. It is modal to the main window but runs in the main message
// loop: its answers reach the workflow through answer.
type planDialog struct {
	a   *app
	win *dialogWindow

	plan *interact.BackupPlan
	opts *interact.BackupStartOptions
	// answer answers ConfirmBackupStart; nil while no question is open.
	answer      func(interact.BackupStart, error)
	showRemoves bool
	view        view.BackupPlanView
	start       win32.HWND
	// table lists the folders, with the splitter that sets its height; it
	// outlives the rebuilds, so the widths the user gives its columns stay.
	table *dialogTable

	// The layout of the last build: the stack and the buttons at the left
	// and right.
	st          *stack
	left, right []win32.HWND
}

// openPlanDialog opens the plan dialog in its waiting state.
func (a *app) openPlanDialog() {
	if a.plan != nil {
		return
	}
	win, err := newDialogWindow(a.theme, a.hwnd, planClass, view.PlanTitle)
	if err != nil {
		return
	}
	p := &planDialog{a: a, win: win}
	p.table = newDialogTable(win.theme, win.panel.HWND(), func() { p.layout(false) })
	win.panel.OnNotify = func(hdr *win32.NMHdr) uintptr {
		r, _ := p.table.notify(hdr)
		return r
	}
	win.onDpi = func(widget.Scale) {
		p.table.restyle()
		p.build(false)
	}
	win.onCommand = p.command
	win.defID = func() uint16 {
		if p.start != 0 && win32.IsEnabled(p.start) {
			return idPlanStart
		}
		return 0
	}
	a.plan = p
	p.build(true)
	win32.Enable(a.hwnd, false)
	win32.ShowWindow(win.hwnd, win32.SW_SHOWNORMAL)
	p.focus()
}

// setPlan shows a plan; the buttons wait for the question.
func (p *planDialog) setPlan(plan interact.BackupPlan) {
	p.plan, p.opts = &plan, nil
	p.build(false)
}

// ask shows the choices and waits for the user.
func (p *planDialog) ask(opts interact.BackupStartOptions, answer func(interact.BackupStart, error)) {
	p.opts, p.answer = &opts, answer
	p.build(false)
	p.focus()
}

// build creates the controls for the current state and sizes the dialog.
func (p *planDialog) build(place bool) {
	a := p.a
	t := p.win.theme
	s := t.Scale
	pal := t.Palette
	panel := p.win.panel
	panel.Clear()
	p.start = 0
	st := newStack(t, panel, s.Px(planWidth-2*planMargin))
	p.st = st
	p.table.begin(st, p.plan != nil)
	if p.plan == nil {
		p.view = view.BackupPlanView{Cancel: view.Button{Text: view.ButtonCancel, Action: view.ActionCancel, Enabled: true}}
		st.para(view.PlanPreparing, widget.TextBody, pal.Text, view.GlyphNone)
		st.gap(8)
		if bar, err := widget.NewProgressBar(panel.HWND()); err == nil {
			panel.Adopt(bar.HWND())
			win32.SetAccessibleName(bar.HWND(), view.PlanPreparing)
			bar.Set(-1)
			st.row(10, cell{hwnd: bar.HWND(), fill: true})
		}
	} else {
		p.view = view.BackupPlanOf(*p.plan, p.opts, a.opts.Config, time.Now())
		p.content(st)
	}

	// The buttons: the plan choices at the left, Start and Cancel at the
	// right.
	v := p.view
	var left, right []win32.HWND
	waiting := p.answer == nil
	if v.Full != nil {
		left = append(left, p.button(*v.Full, idPlanFull, false, waiting))
	}
	if v.NewKeys != nil {
		left = append(left, p.button(*v.NewKeys, idPlanNewKeys, false, waiting))
	}
	if v.Start != nil {
		p.start = p.button(*v.Start, idPlanStart, true, waiting)
		right = append(right, p.start)
	}
	right = append(right, p.button(v.Cancel, idPlanCancel, false, false))
	p.left, p.right = left, right
	p.layout(place)
}

// layout sizes the dialog to its content, at most the height of the
// screen, and places the controls; place centers it over the main window.
func (p *planDialog) layout(place bool) {
	t := p.win.theme
	s := t.Scale
	st := p.st
	margin := s.Px(planMargin)
	buttonsH := s.Px(widget.ButtonHeight)
	w := s.Px(planWidth)
	h := p.table.fit(margin + st.height() + margin + buttonsH + margin)
	p.win.resize(w, h, place)
	st.place(margin, margin)
	left, right := p.left, p.right
	row := widget.NewArea(s, win32.Rect{Left: margin, Top: h - margin - buttonsH, Right: w - margin, Bottom: h - margin})
	for i := len(right) - 1; i >= 0; i-- {
		win32.SetWindowPos(right[i], row.RightPx(buttonWidth(t, right[i])))
		row.Right(8)
	}
	x := margin
	for _, b := range left {
		bw := buttonWidth(t, b)
		win32.SetWindowPos(b, win32.Rect{Left: x, Top: h - margin - buttonsH, Right: x + bw, Bottom: h - margin})
		x += bw + s.Px(8)
	}
}

// content adds the plan: heading, folders, lines, issues.
func (p *planDialog) content(st *stack) {
	t := p.win.theme
	pal := t.Palette
	v := p.view
	panel := p.win.panel
	st.row(stackLineHeight, cell{hwnd: panel.PathLabel(v.Heading, widget.TextStrong, pal.Text), fill: true})
	if v.KeysNote != "" {
		st.gap(4)
		st.para(v.KeysNote, widget.TextSmall, pal.AccentText, view.GlyphInfo)
	}
	st.gap(10)
	p.table.set(v.Table())
	p.table.add(st)
	for _, line := range []view.PlanLine{v.Space, v.Unlock, v.Afterwards} {
		color := pal.Text
		if line.Tone != view.ToneNeutral && line.Tone != view.ToneSuccess {
			color = toneColor(pal, line.Tone)
		}
		st.labeled(line.Label, line.Text, color, line.Glyph)
		st.gap(4)
	}
	if v.RemovesLink != "" {
		st.row(stackLineHeight, cell{dip: stackLabelWidth}, cell{hwnd: p.link(view.Button{Text: v.RemovesLink, Enabled: true}, idPlanRemoves), fill: true})
		if p.showRemoves {
			for _, r := range v.Removes {
				st.row(stackLineHeight, cell{dip: stackLabelWidth}, cell{hwnd: panel.Label(r, widget.TextSmall, pal.TextSecondary), fill: true})
			}
		}
	}
	if v.Note != "" {
		st.gap(8)
		st.para(v.Note, widget.TextSmall, pal.TextSecondary, view.GlyphInfo)
	}
	for _, issue := range v.Issues {
		st.gap(6)
		st.para(issue.Text, widget.TextBody, toneColor(pal, issue.Tone), issue.Glyph)
	}
	st.gap(8)
	st.row(stackLineHeight, cell{hwnd: p.link(v.Details, idPlanDetails), dip: 120})
}

// button creates b; disabled keeps it greyed while the workflow works.
func (p *planDialog) button(b view.Button, id uint16, primary, disabled bool) win32.HWND {
	var h win32.HWND
	if primary {
		h = p.win.panel.PrimaryButton(b.Text, uintptr(id))
	} else {
		h = p.win.panel.Button(b.Text, uintptr(id))
	}
	win32.Enable(h, b.Enabled && !disabled)
	return h
}

func (p *planDialog) link(b view.Button, id uint16) win32.HWND {
	h := p.win.panel.Link(b.Text, uintptr(id))
	win32.Enable(h, b.Enabled)
	return h
}

// focus puts the focus on Start, or on Cancel while the workflow works.
func (p *planDialog) focus() {
	if p.start != 0 && win32.IsEnabled(p.start) {
		win32.SetFocus(p.start)
		return
	}
	win32.SetFocus(p.win.hwnd)
}

func (p *planDialog) command(id uint16) {
	a := p.a
	switch id {
	case idPlanStart:
		if p.answer == nil {
			return
		}
		answer := p.takeAnswer()
		p.close()
		a.runStarted()
		answer(interact.BackupAsPlanned, nil)
	case idPlanFull:
		if p.answer == nil {
			return
		}
		choice := interact.BackupFull
		if p.view.Full != nil && p.view.Full.Action == view.ActionAutomaticPlan {
			choice = interact.BackupAutomatic
		}
		p.takeAnswer()(choice, nil)
		p.build(false)
	case idPlanNewKeys:
		if p.answer == nil || !a.confirm(p.win.hwnd, view.NewKeysConfirm(a.opts.Config)) {
			return
		}
		p.takeAnswer()(interact.BackupNewKeys, nil)
		p.build(false)
	case idPlanCancel, win32.IDCANCEL:
		if p.answer != nil {
			answer := p.takeAnswer()
			p.close()
			answer(interact.BackupCancel, nil)
			return
		}
		p.close()
		a.cancelRun()
	case idPlanDetails:
		if p.plan != nil {
			a.showDetails(p.win.hwnd, view.PlanDetailsTitle, p.plan.Details)
		}
	case idPlanRemoves:
		p.showRemoves = !p.showRemoves
		p.build(false)
	}
}

// takeAnswer returns the pending answer and forgets it, so it is given once.
func (p *planDialog) takeAnswer() func(interact.BackupStart, error) {
	answer := p.answer
	p.answer = nil
	return answer
}

// close closes the dialog. A pending question is answered by the bridge
// when the operation is cancelled.
func (p *planDialog) close() {
	a := p.a
	if a.plan != p {
		return
	}
	a.plan = nil
	win32.Enable(a.hwnd, true)
	p.win.destroy()
	a.focusPage()
}
