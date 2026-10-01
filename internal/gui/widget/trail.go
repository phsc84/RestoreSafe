package widget

import (
	"RestoreSafe/internal/gui/win32"
	"fmt"
)

// StepState is where a step of a trail is.
type StepState int

const (
	StepWaiting StepState = iota
	StepCurrent
	StepDone
)

// TrailStep is one step of a trail.
type TrailStep struct {
	Text  string
	State StepState
}

// Sizes of the trail, in DIPs.
const (
	trailGap         = 8
	trailPillPadding = 8
)

// Trail draws the steps of an operation in a row: done steps with a check,
// the current one highlighted, the waiting ones in secondary text (spec
// BR-2).
type Trail struct {
	hwnd  win32.HWND
	theme *Theme
	back  Color
	steps []TrailStep
}

// NewTrail creates a trail on parent, whose background is back.
func NewTrail(t *Theme, parent win32.HWND, back Color) (*Trail, error) {
	tr := &Trail{theme: t, back: back}
	hwnd, err := create(tr, classTrail, parent, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	tr.hwnd = hwnd
	return tr, nil
}

// HWND returns the trail's window.
func (tr *Trail) HWND() win32.HWND { return tr.hwnd }

// Set shows steps. Screen readers announce the current step, e.g. "Step 2
// of 4, Back up 2 of 3".
func (tr *Trail) Set(steps []TrailStep) {
	tr.steps = steps
	name := ""
	for i, s := range steps {
		if s.State == StepCurrent {
			name = fmt.Sprintf("Step %d of %d, %s", i+1, len(steps), s.Text)
		}
	}
	win32.SetAccessibleName(tr.hwnd, name)
	win32.Invalidate(tr.hwnd)
}

func (tr *Trail) paint(hdc uintptr, r win32.Rect) {
	fill(hdc, r, tr.back)
	t := tr.theme
	pal := t.Palette
	s := t.Scale
	flags := uint32(win32.DT_LEFT | win32.DT_VCENTER | win32.DT_SINGLELINE | win32.DT_NOPREFIX)
	x := r.Left
	sep := "›"
	sepW, _ := t.Fonts.Measure(sep, TextSmall)
	check := t.Fonts.GlyphText(GlyphCheck)
	checkStyle := TextIconSmall
	if !t.Fonts.Glyphs {
		checkStyle = TextSmall
	}
	checkW, _ := t.Fonts.Measure(check, checkStyle)
	for i, step := range tr.steps {
		if i > 0 {
			text(hdc, t, sep, win32.Rect{Left: x, Top: r.Top, Right: x + sepW, Bottom: r.Bottom}, TextSmall, pal.TextSecondary, flags)
			x += sepW + s.Px(trailGap)
		}
		style, color := TextSmall, pal.TextSecondary
		switch step.State {
		case StepDone:
			text(hdc, t, check, win32.Rect{Left: x, Top: r.Top, Right: x + checkW, Bottom: r.Bottom}, checkStyle, pal.Success, flags)
			x += checkW + s.Px(4)
			color = pal.Text
		case StepCurrent:
			style, color = TextStrong, pal.AccentText
		}
		w, _ := t.Fonts.Measure(step.Text, style)
		if step.State == StepCurrent {
			pill := win32.Rect{Left: x, Top: r.Top, Right: x + w + 2*s.Px(trailPillPadding), Bottom: r.Bottom}
			pill.Bottom--
			roundRect(hdc, pill, s.Px(ControlRadius*2), pal.Selection, pal.Selection)
			x += s.Px(trailPillPadding)
		}
		text(hdc, t, step.Text, win32.Rect{Left: x, Top: r.Top, Right: x + w, Bottom: r.Bottom}, style, color, flags)
		x += w + s.Px(trailGap)
		if step.State == StepCurrent {
			x += s.Px(trailPillPadding)
		}
	}
}

func (tr *Trail) message(win32.HWND, uint32, uintptr, uintptr) (uintptr, bool) { return 0, false }

// ProgressBar is the standard progress bar, smooth or as a marquee while
// the total is unknown (spec 3.4).
type ProgressBar struct {
	hwnd    win32.HWND
	marquee bool
}

// progressRange is the resolution of the bar.
const progressRange = 1000

// NewProgressBar creates a progress bar on parent.
func NewProgressBar(parent win32.HWND) (*ProgressBar, error) {
	hwnd, err := win32.CreateWindow(0, win32.PROGRESS_CLASS, "", win32.WS_CHILD|win32.WS_VISIBLE, 0, 0, 0, 0, parent, 0)
	if err != nil {
		return nil, err
	}
	win32.SendMessage(hwnd, win32.PBM_SETRANGE32, 0, progressRange)
	return &ProgressBar{hwnd: hwnd}, nil
}

// HWND returns the bar's window.
func (p *ProgressBar) HWND() win32.HWND { return p.hwnd }

// Set shows fraction (0 to 1); a negative fraction shows a marquee.
func (p *ProgressBar) Set(fraction float64) {
	on := fraction < 0
	if on != p.marquee {
		style := win32.Style(p.hwnd)
		if on {
			win32.SetStyle(p.hwnd, style|win32.PBS_MARQUEE)
			win32.SendMessage(p.hwnd, win32.PBM_SETMARQUEE, 1, 30)
		} else {
			win32.SendMessage(p.hwnd, win32.PBM_SETMARQUEE, 0, 0)
			win32.SetStyle(p.hwnd, style&^win32.PBS_MARQUEE)
			win32.SendMessage(p.hwnd, win32.PBM_SETRANGE32, 0, progressRange)
		}
		p.marquee = on
	}
	if !on {
		win32.SendMessage(p.hwnd, win32.PBM_SETPOS, uintptr(min(fraction, 1)*progressRange), 0)
	}
}
