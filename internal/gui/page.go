package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
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
