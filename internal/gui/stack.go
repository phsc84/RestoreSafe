package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// Sizes of stacked items, in DIPs.
const (
	stackLineHeight = 20
	stackLabelWidth = 90
	// cellGap is the gap between the cells of a row.
	cellGap = 8
	// minWrapWidth is the narrowest width text is measured at: before the
	// first layout, a page has no width.
	minWrapWidth = 80
)

// stack lays out controls from top to bottom: rows of cells, wrapped text
// and gaps. Text is measured at the stack's width when it is added, so
// every height is known before the layout: a dialog sizes itself to its
// content, a card tells the page its height.
type stack struct {
	t     *widget.Theme
	panel *widget.Panel
	width int32 // pixels
	// spacing is the gap in DIPs between two rows that no gap separates.
	spacing int32
	items   []stackItem
}

type stackItem struct {
	// before is the spacing above the item, height its height, in pixels.
	before, height int32
	place          func(r win32.Rect)
	gap            bool
}

// cell is a control in a row: of a width in DIPs or pixels, or filling the
// space the other cells leave; height (DIPs) centers a lower control.
type cell struct {
	hwnd win32.HWND
	// icon is drawn in front of hwnd, within the cell, on its first line.
	icon win32.HWND
	// text, in style, is what hwnd shows when it wraps: the row grows to fit
	// it.
	text  string
	style widget.TextStyle
	// place, when set, places the cell instead of moving hwnd: a table
	// fits its columns.
	place func(win32.Rect)
	dip   int32
	px    int32
	fill  bool
	// weight is the share of a fill cell in the free width; 0 counts as 1.
	weight int32
	height int32
	// top puts a cell of height at the row's top instead of its middle:
	// next to text that wraps, it stays on the first line.
	top bool
}

func newStack(t *widget.Theme, panel *widget.Panel, widthPx int32) *stack {
	return &stack{t: t, panel: panel, width: widthPx}
}

// gap adds empty space.
func (s *stack) gap(dip int32) {
	s.items = append(s.items, stackItem{height: s.t.Scale.Px(dip), gap: true})
}

// add adds an item height pixels high, with the spacing above it when it
// follows another row.
func (s *stack) add(height int32, place func(r win32.Rect)) {
	it := stackItem{height: height, place: place}
	if n := len(s.items); n > 0 && !s.items[n-1].gap {
		it.before = s.t.Scale.Px(s.spacing)
	}
	s.items = append(s.items, it)
}

// row adds a row of cells, at least dip high: a row with text grows to fit
// it at the width its cell gets.
func (s *stack) row(dip int32, cells ...cell) {
	sc := s.t.Scale
	height := sc.Px(dip)
	widths := cellWidths(sc, s.width, cells)
	for i, c := range cells {
		height = max(height, sc.Px(c.height))
		if c.text == "" {
			continue
		}
		w := widths[i]
		if c.icon != 0 {
			w -= sc.Px(iconWidth)
		}
		height = max(height, s.t.Fonts.MeasureWrapped(c.text, c.style, max(w, sc.Px(minWrapWidth))))
	}
	s.add(height, func(r win32.Rect) { layoutRow(sc, r, cells) })
}

// text returns a cell of text that wraps, filling the row.
func (s *stack) text(text string, style widget.TextStyle, color widget.Color) cell {
	return cell{hwnd: s.panel.Paragraph(text, style, color), text: text, style: style, fill: true}
}

// para adds text that wraps over the full width; an icon glyph goes in
// front of it.
func (s *stack) para(text string, style widget.TextStyle, color widget.Color, glyph view.Glyph) win32.HWND {
	c := s.text(text, style, color)
	c.icon = s.icon(glyph, color)
	s.row(stackLineHeight, c)
	return c.hwnd
}

// labeled adds a line with a label in front: "Space   About 55 GB needed".
func (s *stack) labeled(label, text string, color widget.Color, glyph view.Glyph) {
	l := labelColumn(s.panel.Label(label, widget.TextSmall, s.t.Palette.TextSecondary))
	l.height, l.top = stackLineHeight, true
	c := s.text(text, widget.TextBody, color)
	c.icon = s.icon(glyph, color)
	s.row(stackLineHeight, l, c)
}

// labelColumn returns the cell of a label in front of a value, or an empty
// one (hwnd 0): the value starts stackLabelWidth from the left.
func labelColumn(hwnd win32.HWND) cell {
	return cell{hwnd: hwnd, dip: stackLabelWidth - cellGap}
}

// icon creates a small icon in color on the stack's panel; 0 for no glyph.
func (s *stack) icon(glyph view.Glyph, color widget.Color) win32.HWND {
	if glyph == view.GlyphNone {
		return 0
	}
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
		h += it.before + it.height
	}
	return h
}

// place lays the items out from (left, top), width pixels wide.
func (s *stack) place(left, top, width int32) {
	y := top
	for _, it := range s.items {
		y += it.before
		if it.place != nil {
			it.place(win32.Rect{Left: left, Top: y, Right: left + width, Bottom: y + it.height})
		}
		y += it.height
	}
}

// cellWidths returns the widths in pixels the cells get in a row width
// pixels wide: fill cells share what the others leave.
func cellWidths(s widget.Scale, width int32, cells []cell) []int32 {
	fixed, fills := int32(0), int32(0)
	for _, c := range cells {
		switch {
		case c.fill:
			fills += c.share()
		case c.px > 0:
			fixed += c.px
		default:
			fixed += s.Px(c.dip)
		}
	}
	fixed += s.Px(cellGap) * int32(max(len(cells)-1, 0))
	fillW := int32(0)
	if fills > 0 {
		fillW = max(width-fixed, 0) / fills
	}
	widths := make([]int32, len(cells))
	for i, c := range cells {
		switch {
		case c.fill:
			widths[i] = fillW * c.share()
		case c.px > 0:
			widths[i] = c.px
		default:
			widths[i] = s.Px(c.dip)
		}
	}
	return widths
}

// layoutRow places the cells of a row from the left.
func layoutRow(s widget.Scale, r win32.Rect, cells []cell) {
	x := r.Left
	for i, w := range cellWidths(s, r.Width(), cells) {
		c := cells[i]
		cr := win32.Rect{Left: x, Top: r.Top, Right: min(x+w, r.Right), Bottom: r.Bottom}
		if c.height > 0 {
			h := s.Px(c.height)
			if !c.top {
				cr.Top += (r.Height() - h) / 2
			}
			cr.Bottom = cr.Top + h
		}
		if c.icon != 0 {
			iw := s.Px(iconWidth)
			bottom := cr.Top + min(cr.Height(), s.Px(stackLineHeight))
			win32.SetWindowPos(c.icon, win32.Rect{Left: cr.Left, Top: cr.Top, Right: cr.Left + iw, Bottom: bottom})
			cr.Left += iw
		}
		switch {
		case c.place != nil:
			c.place(cr)
		case c.hwnd != 0:
			win32.SetWindowPos(c.hwnd, cr)
		}
		x += w + s.Px(cellGap)
	}
}

// share returns the weight of a fill cell.
func (c cell) share() int32 {
	return max(c.weight, 1)
}
