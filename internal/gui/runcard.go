package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// Control IDs of the run card.
const (
	idRunCancel = 420 + iota
	idRunLog
	idRunDetails
	idRunDone
)

// Sizes of the run card, in DIPs.
const (
	runTitleHeight = 26
	runTrailHeight = 24
	runLineHeight  = 20
	runBarHeight   = 10
	runGap         = 6
	runIconSize    = 40
)

// runMode is what the run card shows.
type runMode int

const (
	runHidden runMode = iota
	runProgress
	runResult
)

// runCard is the operation on the Overview in place of the hero (spec
// OV-7): the progress card while it runs (6.2), then its result (6.3).
type runCard struct {
	a    *app
	card *card
	mode runMode
	acts actions

	// Progress mode.
	title, line, bytes, left win32.HWND
	cancel, log              win32.HWND
	trail                    *widget.Trail
	bar                      *widget.ProgressBar

	// Result mode.
	result     view.ResultCard
	icon       *widget.Icon
	lines      []win32.HWND
	resultBtns []win32.HWND
	details    win32.HWND
}

func newRunCard(a *app, parent win32.HWND) (*runCard, error) {
	r := &runCard{a: a, acts: actions{}}
	c, err := newCard(a.theme, parent, 0, r.acts)
	if err != nil {
		return nil, err
	}
	r.card = c
	c.panel.OnCommand = func(id, code uint16) {
		if action, ok := r.acts[id]; ok && (code == win32.BN_CLICKED || code == 0) {
			a.do(action)
		}
	}
	c.panel.Show(false)
	return r, nil
}

// showProgress shows v, creating the progress controls when the card
// showed something else.
func (r *runCard) showProgress(v view.ProgressCard) {
	t := r.a.theme
	p := r.card.panel
	if r.mode != runProgress {
		r.card.reset()
		r.mode = runProgress
		r.title = p.Label("", widget.TextTitle, t.Palette.Text)
		r.cancel = r.acts.button(p, v.Cancel, idRunCancel, false)
		var err error
		if r.trail, err = widget.NewTrail(t, p.HWND(), t.Palette.Surface); err == nil {
			p.Adopt(r.trail.HWND())
		}
		r.line = p.Label("", widget.TextBody, t.Palette.Text)
		if r.bar, err = widget.NewProgressBar(p.HWND()); err == nil {
			p.Adopt(r.bar.HWND())
			win32.SetAccessibleName(r.bar.HWND(), "Progress")
		}
		r.bytes = p.Label("", widget.TextSmall, t.Palette.TextSecondary)
		r.left = p.RightLabel("", widget.TextSmall, t.Palette.TextSecondary)
		r.log = r.acts.link(p, v.Log, idRunLog)
		p.Show(true)
	}
	win32.SetText(r.title, v.Title)
	win32.SetText(r.cancel, v.Cancel.Text)
	win32.Enable(r.cancel, v.Cancel.Enabled)
	if r.trail != nil {
		var steps []widget.TrailStep
		for _, s := range v.Steps {
			steps = append(steps, widget.TrailStep{Text: s.Text, State: widget.StepState(s.State)})
		}
		r.trail.Set(steps)
	}
	win32.SetText(r.line, v.Line)
	if r.bar != nil {
		r.bar.Set(v.Fraction)
	}
	bytes := v.Bytes
	if v.Speed != "" {
		if bytes != "" {
			bytes += " · "
		}
		bytes += v.Speed
	}
	win32.SetText(r.bytes, bytes)
	win32.SetText(r.left, v.Left)
}

// showResult shows the result v.
func (r *runCard) showResult(v view.ResultCard) {
	t := r.a.theme
	p := r.card.panel
	r.card.reset()
	r.mode = runResult
	r.result = v
	r.lines = nil
	r.resultBtns = nil
	r.details = 0
	var err error
	if r.icon, err = widget.NewIcon(t, p.HWND(), t.Palette.Surface, widget.TextIconSmall); err == nil {
		p.Adopt(r.icon.HWND())
		fore, circle := heroColors(t.Palette, v.Tone)
		r.icon.Set(glyphOf(v.Glyph), fore, circle, v.Title)
	}
	r.title = p.Label(v.Title, widget.TextTitle, t.Palette.Text)
	for _, line := range v.Lines {
		r.lines = append(r.lines, p.Label(line, widget.TextBody, t.Palette.Text))
	}
	if v.Details != nil {
		r.details = r.acts.link(p, *v.Details, idRunDetails)
	}
	if v.Log != nil {
		r.resultBtns = append(r.resultBtns, r.acts.button(p, *v.Log, idRunLog, false))
	}
	r.resultBtns = append(r.resultBtns, r.acts.button(p, v.Done, idRunDone, true))
	p.Show(true)
}

// hide hides the card.
func (r *runCard) hide() {
	if r.mode == runHidden {
		return
	}
	r.card.reset()
	r.mode = runHidden
	r.trail, r.bar, r.icon = nil, nil, nil
	r.card.panel.Show(false)
}

// height returns the height the card needs, in DIPs.
func (r *runCard) height() int32 {
	switch r.mode {
	case runProgress:
		return 2*widget.CardPadding + runTitleHeight + runTrailHeight + 2*runLineHeight + runBarHeight + 4*runGap
	case runResult:
		lines := int32(len(r.lines))
		if r.details != 0 {
			lines++
		}
		return 2*widget.CardPadding + max(runTitleHeight+runGap+lines*runLineHeight, runIconSize)
	}
	return 0
}

// place puts the card at rect and lays out its controls.
func (r *runCard) place(rect win32.Rect) {
	p := r.card.panel
	win32.SetWindowPos(p.HWND(), rect)
	t := r.a.theme
	s := t.Scale
	area := widget.NewArea(s, win32.ClientRect(p.HWND()))
	area.Inset(widget.CardPadding, widget.CardPadding, widget.CardPadding, widget.CardPadding)
	switch r.mode {
	case runProgress:
		top := widget.NewArea(s, area.Top(runTitleHeight))
		win32.SetWindowPos(r.cancel, top.RightPx(buttonWidth(t, r.cancel)))
		win32.SetWindowPos(r.title, top.Rest())
		area.Top(runGap)
		if r.trail != nil {
			win32.SetWindowPos(r.trail.HWND(), area.Top(runTrailHeight))
		}
		area.Top(runGap)
		win32.SetWindowPos(r.line, area.Top(runLineHeight))
		area.Top(runGap)
		if r.bar != nil {
			win32.SetWindowPos(r.bar.HWND(), area.Top(runBarHeight))
		}
		area.Top(runGap)
		bottom := widget.NewArea(s, area.Top(runLineHeight))
		linkW, _ := t.Fonts.Measure(win32.Text(r.log), widget.TextSmall)
		win32.SetWindowPos(r.log, bottom.RightPx(linkW+s.Px(linkPadding)))
		bottom.Right(12)
		halves := bottom.Columns(12, 1, 1)
		win32.SetWindowPos(r.bytes, halves[0].Rest())
		win32.SetWindowPos(r.left, halves[1].Rest())
	case runResult:
		if r.icon != nil {
			icon := area.Left(runIconSize)
			icon.Bottom = icon.Top + s.Px(runIconSize)
			win32.SetWindowPos(r.icon.HWND(), icon)
			area.Left(12)
		}
		top := widget.NewArea(s, area.Top(runTitleHeight))
		for i := len(r.resultBtns) - 1; i >= 0; i-- {
			win32.SetWindowPos(r.resultBtns[i], top.RightPx(buttonWidth(t, r.resultBtns[i])))
			top.Right(8)
		}
		win32.SetWindowPos(r.title, top.Rest())
		area.Top(runGap)
		for _, l := range r.lines {
			win32.SetWindowPos(l, area.Top(runLineHeight))
		}
		if r.details != 0 {
			linkW, _ := t.Fonts.Measure(r.result.Details.Text, widget.TextSmall)
			row := area.Top(runLineHeight)
			row.Right = min(row.Left+linkW+s.Px(linkPadding), row.Right)
			win32.SetWindowPos(r.details, row)
		}
	}
}

// focus puts the keyboard focus on the card's main button.
func (r *runCard) focus() {
	switch r.mode {
	case runProgress:
		win32.SetFocus(r.cancel)
	case runResult:
		win32.SetFocus(r.resultBtns[len(r.resultBtns)-1])
	}
}

// buttonWidth is the width of a button for its text.
func buttonWidth(t *widget.Theme, h win32.HWND) int32 {
	w, _ := t.Fonts.Measure(win32.Text(h), widget.TextBody)
	return max(w+t.Scale.Px(buttonPadding), t.Scale.Px(minButtonWidth))
}
