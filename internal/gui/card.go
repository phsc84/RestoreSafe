package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// card is a framed panel whose rows are a stack, rebuilt on every update.
type card struct {
	*stack
	linkID uint16
	acts   actions
}

func newCard(t *widget.Theme, parent win32.HWND, linkID uint16, acts actions) (*card, error) {
	p, err := widget.NewPanel(t, parent, 0, widget.PanelStyle{Back: t.Palette.Surface, Outer: t.Palette.Surface, Card: true})
	if err != nil {
		return nil, err
	}
	st := newStack(t, p, 0)
	st.spacing = rowGap
	return &card{stack: st, linkID: linkID, acts: acts}, nil
}

// tip shows text when the mouse rests on hwnd.
func (c *card) tip(hwnd win32.HWND, text string) win32.HWND {
	c.panel.Tip(hwnd, text)
	return hwnd
}

// reset clears the card for rows built at a card width of width pixels; 0
// when none of its text wraps.
func (c *card) reset(width int32) {
	c.panel.Clear()
	c.items = nil
	c.width = max(width-2*c.t.Scale.Px(widget.CardPadding), 0)
}

func (c *card) label(text string, style widget.TextStyle, color widget.Color) win32.HWND {
	return c.panel.Label(text, style, color)
}

// heading adds the card's title and, with link, its link at the right.
func (c *card) heading(title string, link *view.Button) {
	cells := []cell{{hwnd: c.label(title, widget.TextStrong, c.t.Palette.Text), fill: true}}
	if link != nil && link.Text != "" {
		w, _ := c.t.Fonts.Measure(link.Text, widget.TextSmall)
		cells = append(cells, cell{hwnd: c.acts.link(c.panel, *link, c.linkID), px: w + c.t.Scale.Px(linkPadding)})
	}
	c.row(cardHeadingHeight, cells...)
}

// height returns the height the card needs, in pixels.
func (c *card) height() int32 {
	return c.stack.height() + 2*c.t.Scale.Px(widget.CardPadding)
}

// place puts the card at r and lays out its rows.
func (c *card) place(r win32.Rect) {
	win32.SetWindowPos(c.panel.HWND(), r)
	pad := c.t.Scale.Px(widget.CardPadding)
	c.stack.place(pad, pad, r.Width()-2*pad)
}

// contentWidth returns the width in pixels of a page's content: its client
// width less the padding at both sides.
func contentWidth(t *widget.Theme, page win32.HWND) int32 {
	return win32.ClientRect(page).Width() - 2*t.Scale.Px(widget.ContentPaddingX)
}

// lineHeight returns the height of a line of text in style, in DIPs.
func lineHeight(t *widget.Theme, style widget.TextStyle) int32 {
	_, h := t.Fonts.Measure("Ag", style)
	return (h*96 + int32(t.Scale) - 1) / int32(t.Scale)
}
