package gui

import (
	"strings"

	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// hero is the top of a page that says how things are: a glyph in a circle,
// a title, a line that wraps, and buttons at the right.
type hero struct {
	theme       *widget.Theme
	icon        *widget.Icon
	title, line win32.HWND
	// buttons are in tab order, left to right; the first is the primary.
	buttons []win32.HWND
}

// newHero creates a hero on panel with a button per id. Its controls follow
// the panel's earlier ones in the tab order.
func newHero(t *widget.Theme, panel *widget.Panel, acts actions, ids ...uint16) (*hero, error) {
	icon, err := widget.NewIcon(t, panel.HWND(), t.Palette.Surface, widget.TextIcon)
	if err != nil {
		return nil, err
	}
	h := &hero{theme: t, icon: icon}
	h.title = panel.Label("", widget.TextHero, t.Palette.Text)
	h.line = panel.Paragraph("", widget.TextSmall, t.Palette.TextSecondary)
	for i, id := range ids {
		h.buttons = append(h.buttons, acts.button(panel, view.Button{Text: " "}, id, i == 0))
	}
	return h, nil
}

// set shows the glyph, the title and the line of v; the page sets the
// buttons.
func (h *hero) set(v view.Hero) {
	fore, circle := heroColors(h.theme.Palette, v.Tone)
	h.icon.Set(glyphOf(v.Glyph), fore, circle, v.Title)
	win32.SetText(h.title, v.Title)
	// A path in the title keeps its end (the folder) when shortened.
	style := win32.Style(h.title) &^ (win32.SS_ENDELLIPSIS | win32.SS_PATHELLIPSIS)
	if strings.Contains(v.Title, `\`) {
		win32.SetStyle(h.title, style|win32.SS_PATHELLIPSIS)
	} else {
		win32.SetStyle(h.title, style|win32.SS_ENDELLIPSIS)
	}
	win32.SetText(h.line, v.Line)
}

// show shows or hides the hero; hidden, its buttons stay hidden until the
// page sets them again.
func (h *hero) show(shown bool) {
	for _, c := range []win32.HWND{h.icon.HWND(), h.title, h.line} {
		win32.SetVisible(c, shown)
	}
	if !shown {
		for _, b := range h.buttons {
			setShown(b, false)
		}
	}
}

// shownButtons are the hero's shown buttons, from the right.
func (h *hero) shownButtons() []win32.HWND {
	var out []win32.HWND
	for i := len(h.buttons) - 1; i >= 0; i-- {
		if win32.IsWindowVisible(h.buttons[i]) {
			out = append(out, h.buttons[i])
		}
	}
	return out
}

// heroGeometry is the layout of the hero's text at a width.
type heroGeometry struct {
	textWidth, textHeight int32
	lineHeight            int32
	height                int32
}

// geometry lays the hero's text out at width pixels: the line wraps when it
// does not fit, and the hero grows with it. Without a line, the title
// alone is centered.
func (h *hero) geometry(width int32) heroGeometry {
	t := h.theme
	s := t.Scale
	g := heroGeometry{textWidth: width - s.Px(widget.HeroIconSize+heroGap+12)}
	for _, b := range h.shownButtons() {
		g.textWidth -= heroButtonWidth(t, b) + s.Px(8)
	}
	g.textWidth = max(g.textWidth, s.Px(200))
	if line := win32.Text(h.line); line != "" {
		g.lineHeight = max(t.Fonts.MeasureWrapped(line, widget.TextSmall, g.textWidth), s.Px(heroLineHeight))
	}
	g.textHeight = s.Px(heroTitleHeight) + g.lineHeight
	g.height = max(s.Px(heroHeight), g.textHeight+s.Px(8))
	return g
}

// place lays the hero out in r.
func (h *hero) place(r win32.Rect) {
	t := h.theme
	s := t.Scale
	g := h.geometry(r.Width())
	area := widget.NewArea(s, r)
	iconRect := area.Left(widget.HeroIconSize)
	iconRect.Top += (iconRect.Height() - s.Px(widget.HeroIconSize)) / 2
	iconRect.Bottom = iconRect.Top + s.Px(widget.HeroIconSize)
	win32.SetWindowPos(h.icon.HWND(), iconRect)
	area.Left(heroGap)
	for _, b := range h.shownButtons() {
		br := area.RightPx(heroButtonWidth(t, b))
		br.Top += (br.Height() - s.Px(widget.ButtonHeight)) / 2
		br.Bottom = br.Top + s.Px(widget.ButtonHeight)
		win32.SetWindowPos(b, br)
		area.Right(8)
	}
	text := area.Rest()
	text.Top += (text.Height() - g.textHeight) / 2
	x, y := text.Left, text.Top
	win32.SetWindowPos(h.title, win32.Rect{Left: x, Top: y, Right: x + g.textWidth, Bottom: y + s.Px(heroTitleHeight)})
	y += s.Px(heroTitleHeight)
	win32.SetWindowPos(h.line, win32.Rect{Left: x, Top: y, Right: x + g.textWidth, Bottom: y + g.lineHeight})
}

func heroButtonWidth(t *widget.Theme, b win32.HWND) int32 {
	w, _ := t.Fonts.Measure(win32.Text(b), widget.TextBody)
	return max(w+t.Scale.Px(buttonPadding), t.Scale.Px(minButtonWidth))
}
