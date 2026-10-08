package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// headHeight returns the height in pixels of the title, the run card and
// the lines above the list, with their gaps, at a page width.
func (b *restorePage) headHeight(width int32) int32 {
	s := b.a.theme.Scale
	h := s.Px(pageTitleHeight) + s.Px(8)
	if b.run.mode != runHidden {
		h += b.run.height(width-2*s.Px(widget.ContentPaddingX)) + s.Px(widget.CardGap)
	}
	if win32.IsWindowVisible(b.lines.HWND()) && b.linesStack != nil {
		h += b.linesHeight + s.Px(widget.CardGap)
	}
	return h
}

// layout places the page's controls. The list fills the page above its
// action bar; the page scrolls when it leaves the list less than
// minListHeight.
func (b *restorePage) layout() {
	t := b.a.theme
	s := t.Scale
	client := win32.ClientRect(b.panel.HWND())
	bar := s.Px(actionBarHeight)
	var listH, total int32
	// A scroll bar that comes or goes changes the width, and with it the
	// run card's height.
	for range 2 {
		fixed := 2*s.Px(widget.ContentPaddingY) + b.headHeight(client.Width()) + bar
		listH = max(client.Height()-fixed, s.Px(minListHeight))
		total = fixed + listH
		b.panel.SetScroll(total)
		now := win32.ClientRect(b.panel.HWND())
		if now.Width() == client.Width() {
			break
		}
		client = now
	}
	page := client
	page.Top -= b.panel.ScrollOffset()
	page.Bottom = page.Top + max(total, client.Height())
	area := widget.NewArea(s, page)
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)
	top := widget.NewArea(s, area.Top(pageTitleHeight))
	f := top.Right(filterWidth)
	f.Bottom = f.Top + s.Px(filterDropHeight)
	win32.SetWindowPos(b.filter, f)
	top.Right(12)
	placeTitle(t, b.title, b.refresh, top.Rest())
	area.Top(8)
	if b.run.mode != runHidden {
		b.run.place(area.TopPx(b.run.height(area.Rest().Width())))
		area.Top(widget.CardGap)
	}

	if win32.IsWindowVisible(b.lines.HWND()) && b.linesStack != nil {
		r := area.TopPx(b.linesHeight)
		win32.SetWindowPos(b.lines.HWND(), r)
		pad := s.Px(widget.CardPadding)
		b.linesStack.place(pad, pad)
		area.Top(widget.CardGap)
	}

	// The list and its action bar.
	bottom := area.Rest()
	y := bottom.Top
	win32.SetWindowPos(b.list, win32.Rect{Left: bottom.Left, Top: y, Right: bottom.Right, Bottom: y + listH})
	y += listH
	barArea := widget.NewArea(s, win32.Rect{Left: bottom.Left, Top: y, Right: bottom.Right, Bottom: y + bar})
	barArea.Inset(0, 4, 0, 4)
	win32.SetWindowPos(b.verify, barArea.RightPx(buttonWidth(t, b.verify)))
	barArea.Right(8)
	win32.SetWindowPos(b.restore, barArea.RightPx(buttonWidth(t, b.restore)))
	barArea.Right(12)
	win32.SetWindowPos(b.barText, barArea.Rest())
}
