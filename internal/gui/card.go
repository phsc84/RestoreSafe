package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// card is a card of rows of cells, rebuilt on every update.
type card struct {
	theme  *widget.Theme
	panel  *widget.Panel
	linkID uint16
	acts   actions
	rows   []cardRow
}

// cardRow is a row of a card, height in DIPs.
type cardRow struct {
	height int32
	cells  []cell
}

// cell is a control in a row: of a width in DIPs or pixels, or filling the
// space the other cells leave; height (DIPs) centers a lower control.
type cell struct {
	hwnd win32.HWND
	// icon is drawn in front of hwnd, within the cell.
	icon win32.HWND
	// place, when set, places the cell instead of moving hwnd: a table
	// fits its columns.
	place func(win32.Rect)
	dip   int32
	px    int32
	fill  bool
	// weight is the share of a fill cell in the free width; 0 counts as 1.
	weight int32
	height int32
}

func newCard(t *widget.Theme, parent win32.HWND, linkID uint16, acts actions) (*card, error) {
	p, err := widget.NewPanel(t, parent, 0, widget.PanelStyle{Back: t.Palette.Surface, Outer: t.Palette.Surface, Card: true})
	if err != nil {
		return nil, err
	}
	return &card{theme: t, panel: p, linkID: linkID, acts: acts}, nil
}

// tip shows text when the mouse rests on hwnd.
func (c *card) tip(hwnd win32.HWND, text string) win32.HWND {
	c.panel.Tip(hwnd, text)
	return hwnd
}

func (c *card) reset() {
	c.panel.Clear()
	c.rows = nil
}

func (c *card) label(text string, style widget.TextStyle, color widget.Color) win32.HWND {
	return c.panel.Label(text, style, color)
}

// heading adds the card's title and, with link, its link at the right.
func (c *card) heading(title string, link *view.Button) {
	cells := []cell{{hwnd: c.label(title, widget.TextStrong, c.theme.Palette.Text), fill: true}}
	if link != nil && link.Text != "" {
		w, _ := c.theme.Fonts.Measure(link.Text, widget.TextSmall)
		cells = append(cells, cell{hwnd: c.acts.link(c.panel, *link, c.linkID), px: w + c.theme.Scale.Px(linkPadding)})
	}
	c.row(cardHeadingHeight, cells...)
}

func (c *card) row(height int32, cells ...cell) {
	c.rows = append(c.rows, cardRow{height: height, cells: cells})
}

// height returns the height the card needs, in DIPs.
func (c *card) height() int32 {
	h := int32(2 * widget.CardPadding)
	for i, r := range c.rows {
		h += r.height
		if i > 0 {
			h += rowGap
		}
	}
	return h
}

// place puts the card at r and lays out its rows.
func (c *card) place(r win32.Rect) {
	win32.SetWindowPos(c.panel.HWND(), r)
	s := c.theme.Scale
	area := widget.NewArea(s, win32.ClientRect(c.panel.HWND()))
	area.Inset(widget.CardPadding, widget.CardPadding, widget.CardPadding, widget.CardPadding)
	for i, row := range c.rows {
		if i > 0 {
			area.Top(rowGap)
		}
		layoutRow(s, area.Top(row.height), row.cells)
	}
}

// layoutRow places the cells of a row from the left; fill cells share the
// width the others leave.
func layoutRow(s widget.Scale, r win32.Rect, cells []cell) {
	gap := s.Px(8)
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
	fixed += gap * int32(max(len(cells)-1, 0))
	fillW := int32(0)
	if fills > 0 {
		fillW = max(r.Width()-fixed, 0) / fills
	}
	x := r.Left
	for _, c := range cells {
		w := s.Px(c.dip)
		switch {
		case c.fill:
			w = fillW * c.share()
		case c.px > 0:
			w = c.px
		}
		cr := win32.Rect{Left: x, Top: r.Top, Right: min(x+w, r.Right), Bottom: r.Bottom}
		if c.height > 0 {
			h := s.Px(c.height)
			cr.Top += (r.Height() - h) / 2
			cr.Bottom = cr.Top + h
		}
		if c.icon != 0 {
			w := s.Px(iconWidth)
			win32.SetWindowPos(c.icon, win32.Rect{Left: cr.Left, Top: cr.Top, Right: cr.Left + w, Bottom: cr.Bottom})
			cr.Left += w
		}
		switch {
		case c.place != nil:
			c.place(cr)
		case c.hwnd != 0:
			win32.SetWindowPos(c.hwnd, cr)
		}
		x += w + gap
	}
}

// share returns the weight of a fill cell.
func (c cell) share() int32 {
	return max(c.weight, 1)
}
