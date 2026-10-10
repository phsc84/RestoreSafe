package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// stack lays out the controls of a dialog from top to bottom in a fixed
// width: rows of cells, wrapped paragraphs and gaps. Every height is known
// when the item is added, so the dialog can size itself to its content.
type stack struct {
	t     *widget.Theme
	panel *widget.Panel
	width int32 // pixels
	items []stackItem
}

type stackItem struct {
	height int32 // pixels
	place  func(r win32.Rect)
}

// Sizes of stacked items, in DIPs.
const (
	stackLineHeight = 20
	stackLabelWidth = 90
)

func newStack(t *widget.Theme, panel *widget.Panel, widthPx int32) *stack {
	return &stack{t: t, panel: panel, width: widthPx}
}

// gap adds empty space.
func (s *stack) gap(dip int32) {
	s.items = append(s.items, stackItem{height: s.t.Scale.Px(dip)})
}

// row adds a row of cells, dip high.
func (s *stack) row(dip int32, cells ...cell) {
	sc := s.t.Scale
	s.items = append(s.items, stackItem{height: sc.Px(dip), place: func(r win32.Rect) { layoutRow(sc, r, cells) }})
}

// para adds text that wraps over the full width; an icon glyph goes in
// front of it.
func (s *stack) para(text string, style widget.TextStyle, color widget.Color, glyph view.Glyph) win32.HWND {
	sc := s.t.Scale
	indent := int32(0)
	var icon win32.HWND
	if glyph != view.GlyphNone {
		icon = s.icon(glyph, color)
		indent = sc.Px(iconWidth)
	}
	h := s.panel.Paragraph(text, style, color)
	height := max(s.t.Fonts.MeasureWrapped(text, style, s.width-indent), sc.Px(stackLineHeight))
	s.items = append(s.items, stackItem{height: height, place: func(r win32.Rect) {
		if icon != 0 {
			win32.SetWindowPos(icon, win32.Rect{Left: r.Left, Top: r.Top, Right: r.Left + indent, Bottom: r.Top + sc.Px(stackLineHeight)})
		}
		r.Left += indent
		win32.SetWindowPos(h, r)
	}})
	return h
}

// labeled adds a line with a label in front: "Space   About 55 GB needed".
func (s *stack) labeled(label, text string, color widget.Color, glyph view.Glyph) {
	sc := s.t.Scale
	l := s.panel.Label(label, widget.TextSmall, s.t.Palette.TextSecondary)
	labelW := sc.Px(stackLabelWidth)
	indent := int32(0)
	var icon win32.HWND
	if glyph != view.GlyphNone {
		icon = s.icon(glyph, color)
		indent = sc.Px(iconWidth)
	}
	h := s.panel.Paragraph(text, widget.TextBody, color)
	height := max(s.t.Fonts.MeasureWrapped(text, widget.TextBody, s.width-labelW-indent), sc.Px(stackLineHeight))
	s.items = append(s.items, stackItem{height: height, place: func(r win32.Rect) {
		win32.SetWindowPos(l, win32.Rect{Left: r.Left, Top: r.Top, Right: r.Left + labelW, Bottom: r.Top + sc.Px(stackLineHeight)})
		r.Left += labelW
		if icon != 0 {
			win32.SetWindowPos(icon, win32.Rect{Left: r.Left, Top: r.Top, Right: r.Left + indent, Bottom: r.Top + sc.Px(stackLineHeight)})
			r.Left += indent
		}
		win32.SetWindowPos(h, r)
	}})
}

// icon creates a small icon in color on the stack's panel.
func (s *stack) icon(glyph view.Glyph, color widget.Color) win32.HWND {
	i, err := widget.NewIcon(s.t, s.panel.HWND(), s.panel.Back(), widget.TextIconSmall)
	if err != nil {
		return 0
	}
	s.panel.Adopt(i.HWND())
	i.Set(glyphOf(glyph), color, widget.NoCircle, "")
	return i.HWND()
}

// height returns the height of the items in pixels.
func (s *stack) height() int32 {
	h := int32(0)
	for _, it := range s.items {
		h += it.height
	}
	return h
}

// place lays the items out from (left, top).
func (s *stack) place(left, top int32) {
	y := top
	for _, it := range s.items {
		if it.place != nil {
			it.place(win32.Rect{Left: left, Top: y, Right: left + s.width, Bottom: y + it.height})
		}
		y += it.height
	}
}
