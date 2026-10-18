package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// showRefresh shows a page's Refresh button b on h, disabled while a check
// or an operation runs: a reload waits for neither (OV-8).
func (a *app) showRefresh(h win32.HWND, b view.Button, acts actions, id uint16) {
	acts[id] = b.Action
	win32.SetText(h, b.Text)
	win32.Enable(h, b.Enabled && !a.checking && !a.reloading && !a.machine.Busy())
}

// placeTitle places a page's title in row and its Refresh button just after
// the title's text, centered on it.
func placeTitle(t *widget.Theme, title, refresh win32.HWND, row win32.Rect) {
	s := t.Scale
	tw, th := t.Fonts.Measure(win32.Text(title), widget.TextTitle)
	rw, _ := t.Fonts.Measure(win32.Text(refresh), widget.TextSmall)
	rw += s.Px(20)
	gap := s.Px(12)
	tr := row
	tr.Right = max(min(row.Left+tw+s.Px(4), row.Right-gap-rw), row.Left)
	win32.SetWindowPos(title, tr)
	r := win32.Rect{Left: tr.Right + gap}
	r.Right = r.Left + rw
	r.Top = max(row.Top+(th-s.Px(refreshHeight))/2, row.Top)
	r.Bottom = r.Top + s.Px(refreshHeight)
	win32.SetWindowPos(refresh, r)
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
