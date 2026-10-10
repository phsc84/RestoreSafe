package gui

import (
	"slices"
	"time"

	"github.com/phsc84/restoresafe/internal/gui/flow"
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// Control IDs of the run card.
const (
	idRunCancel = 420 + iota
	idRunLog
	idRunDetails
	idRunDone
	idRunOpen
	idRunTitle
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

// runCard is the operation at the top of a page: a backup on Create backup
// in place of the hero (GUI spec OV-7), a restore or verification on Restore
// backup. It shows the progress card while the operation runs (6.2), then
// its result (6.3).
type runCard struct {
	a     *app
	theme *widget.Theme
	card  *card
	mode  runMode
	acts  actions
	// resultOf is the run whose result the card shows; decorate adds what
	// the page knows to it.
	resultOf *flow.Run
	decorate func(*view.ResultCard)

	// Progress mode.
	title, line, bytes win32.HWND
	cancel, log        win32.HWND
	trail              *widget.Trail
	bar                *widget.ProgressBar

	// Result mode.
	// do runs the actions of the card's buttons; the app's by default.
	do func(view.Action)

	result     view.ResultCard
	icon       *widget.Icon
	lines      []win32.HWND
	resultBtns []win32.HWND
	details    win32.HWND
}

func newRunCard(a *app, parent win32.HWND) (*runCard, error) {
	return newRunCardWith(a, a.theme, parent)
}

// showProgress shows v, creating the progress controls when the card
// showed something else.
func (r *runCard) showProgress(v view.ProgressCard) {
	t := r.theme
	p := r.card.panel
	if r.mode != runProgress {
		r.card.reset()
		r.mode = runProgress
		r.title = p.Label("", widget.TextTitle, t.Palette.Text)
		win32.SetControlID(r.title, idRunTitle)
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
}

// showResult shows the result v.
func (r *runCard) showResult(v view.ResultCard) {
	t := r.theme
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
	win32.SetControlID(r.title, idRunTitle)
	for _, line := range v.Lines {
		r.lines = append(r.lines, p.Paragraph(line, widget.TextBody, t.Palette.Text))
	}
	if v.Details != nil {
		r.details = r.acts.link(p, *v.Details, idRunDetails)
	}
	if v.Log != nil {
		r.resultBtns = append(r.resultBtns, r.acts.button(p, *v.Log, idRunLog, false))
	}
	if v.Open != nil {
		r.resultBtns = append(r.resultBtns, r.acts.button(p, *v.Open, idRunOpen, false))
	}
	r.resultBtns = append(r.resultBtns, r.acts.button(p, v.Done, idRunDone, true))
	p.Show(true)
}

// follow shows run on the card: its progress while it is busy, then its
// result; nil hides the card. It reports whether the card's mode changed.
func (r *runCard) follow(run *flow.Run, busy bool) bool {
	mode := r.mode
	switch {
	case run != nil && busy:
		r.resultOf = nil
		r.showProgress(view.ProgressCardOf(run, time.Now()))
	case run != nil && run.Stage == flow.StageFinished:
		c := view.ResultCardOf(run)
		if c != nil && r.decorate != nil {
			r.decorate(c)
		}
		switch {
		case c == nil:
			r.hide()
		case r.resultOf != run || !slices.Equal(r.result.Lines, c.Lines):
			// A new result, or the check after it found a problem (BR-7).
			r.showResult(*c)
			r.resultOf = run
		}
	default:
		r.resultOf = nil
		r.hide()
	}
	return r.mode != mode
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

// height returns the height in pixels the card needs at width pixels.
func (r *runCard) height(width int32) int32 {
	s := r.theme.Scale
	switch r.mode {
	case runProgress:
		return s.Px(2*widget.CardPadding + runTitleHeight + runTrailHeight + 2*runLineHeight + runBarHeight + 4*runGap)
	case runResult:
		g := r.resultGeometry(width)
		text := s.Px(runTitleHeight+runGap) + g.linesHeight()
		if r.details != 0 {
			text += s.Px(runLineHeight)
		}
		if g.buttonsBelow {
			text += s.Px(runGap*2) + s.Px(widget.ButtonHeight)
		}
		return 2*s.Px(widget.CardPadding) + max(text, s.Px(runIconSize))
	}
	return 0
}

// resultGeometry is the layout of the result at a card width.
type resultGeometry struct {
	textWidth int32
	// buttonsBelow puts the buttons on their own row when they would leave
	// the title too little room.
	buttonsBelow bool
	lineHeights  []int32
}

func (g resultGeometry) linesHeight() int32 {
	h := int32(0)
	for _, l := range g.lineHeights {
		h += l
	}
	return h
}

func (r *runCard) resultGeometry(width int32) resultGeometry {
	t := r.theme
	s := t.Scale
	g := resultGeometry{textWidth: max(width-2*s.Px(widget.CardPadding)-s.Px(runIconSize+12), s.Px(100))}
	buttons := int32(0)
	for i, b := range r.resultBtns {
		if i > 0 {
			buttons += s.Px(8)
		}
		buttons += buttonWidth(t, b)
	}
	titleW, _ := t.Fonts.Measure(r.result.Title, widget.TextTitle)
	g.buttonsBelow = titleW+s.Px(16)+buttons > g.textWidth
	for _, line := range r.result.Lines {
		g.lineHeights = append(g.lineHeights, max(t.Fonts.MeasureWrapped(line, widget.TextBody, g.textWidth), s.Px(runLineHeight)))
	}
	return g
}

// place puts the card at rect and lays out its controls.
func (r *runCard) place(rect win32.Rect) {
	p := r.card.panel
	win32.SetWindowPos(p.HWND(), rect)
	t := r.theme
	s := t.Scale
	area := widget.NewArea(s, win32.ClientRect(p.HWND()))
	area.Inset(widget.CardPadding, widget.CardPadding, widget.CardPadding, widget.CardPadding)
	// A card taller than its content (the Restore backup window's page) centres it.
	if extra := rect.Height() - r.height(rect.Width()); extra > 0 {
		area.TopPx(extra / 2)
		area.R.Bottom -= extra - extra/2
	}
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
		win32.SetWindowPos(r.bytes, bottom.Rest())
	case runResult:
		g := r.resultGeometry(rect.Width())
		if r.icon != nil {
			icon := area.Left(runIconSize)
			icon.Bottom = icon.Top + s.Px(runIconSize)
			win32.SetWindowPos(r.icon.HWND(), icon)
			area.Left(12)
		}
		// The buttons sit in their own row, or at the right of the title's
		// row, which the title then leaves to them: a label under a
		// button would take its clicks.
		var buttons *widget.Area
		if g.buttonsBelow {
			below := widget.NewArea(s, area.Bottom(widget.ButtonHeight))
			area.Bottom(runGap * 2)
			buttons = &below
		}
		top := widget.NewArea(s, area.Top(runTitleHeight))
		if buttons == nil {
			buttons = &top
		}
		for i := len(r.resultBtns) - 1; i >= 0; i-- {
			b := r.resultBtns[i]
			rr := buttons.RightPx(buttonWidth(t, b))
			if !g.buttonsBelow {
				rr.Top += (rr.Height() - s.Px(widget.ButtonHeight)) / 2
				rr.Bottom = rr.Top + s.Px(widget.ButtonHeight)
			}
			win32.SetWindowPos(b, rr)
			buttons.Right(8)
		}
		win32.SetWindowPos(r.title, top.Rest())
		area.Top(runGap)
		for i, l := range r.lines {
			win32.SetWindowPos(l, area.TopPx(g.lineHeights[i]))
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

// newRunCardWith creates a run card that draws with theme, for a dialog
// with a theme of its own.
func newRunCardWith(a *app, theme *widget.Theme, parent win32.HWND) (*runCard, error) {
	r := &runCard{a: a, theme: theme, acts: actions{}, do: a.do}
	c, err := newCard(theme, parent, 0, r.acts)
	if err != nil {
		return nil, err
	}
	r.card = c
	c.panel.OnCommand = func(id, code uint16) {
		if action, ok := r.acts[id]; ok && (code == win32.BN_CLICKED || code == 0) {
			r.do(action)
		}
	}
	c.panel.Show(false)
	return r, nil
}
