package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// layout places the title, the hero or the run card below it, like on
// Restore backup, and the cards. The page scrolls when they don't fit.
func (o *createPage) layout() {
	t := o.a.theme
	s := t.Scale
	client := win32.ClientRect(o.panel.HWND())
	var total int32
	// A scroll bar that comes or goes changes the width, and with it the
	// hero's height.
	for range 2 {
		total = o.contentHeight(client.Width())
		o.panel.SetScroll(total)
		now := win32.ClientRect(o.panel.HWND())
		if now.Width() == client.Width() {
			break
		}
		client = now
	}
	page := client
	page.Top -= o.panel.ScrollOffset()
	page.Bottom = page.Top + max(total, client.Height())
	area := widget.NewArea(s, page)
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)
	placeTitle(t, o.title, o.refresh, area.Top(pageTitleHeight))
	area.Top(8)

	if o.run.mode != runHidden {
		o.run.place(area.TopPx(o.run.height(area.Rest().Width())))
	} else {
		o.layoutHero(area.TopPx(o.heroGeometry(area.Rest().Width()).height))
	}

	for _, c := range []*card{o.folders, o.storage, o.keys} {
		gap := area.Top(widget.CardGap)
		if c == o.storage {
			o.sizer.place(gap)
		}
		c.place(area.Top(c.height()))
	}
}

// contentHeight returns the height in pixels of the page's content at a
// page width.
func (o *createPage) contentHeight(width int32) int32 {
	s := o.a.theme.Scale
	inner := width - 2*s.Px(widget.ContentPaddingX)
	h := 2*s.Px(widget.ContentPaddingY) + s.Px(pageTitleHeight) + s.Px(8)
	if o.run.mode != runHidden {
		h += o.run.height(inner)
	} else {
		h += o.heroGeometry(inner).height
	}
	for _, c := range []*card{o.folders, o.storage, o.keys} {
		h += s.Px(widget.CardGap) + s.Px(c.height())
	}
	return h
}

// measure returns the width text needs in style, with a little room.
func measure(t *widget.Theme, text string, style widget.TextStyle) int32 {
	w, _ := t.Fonts.Measure(text, style)
	return w + t.Scale.Px(4)
}

// layoutHero places the hero in r.
func (o *createPage) layoutHero(r win32.Rect) {
	t := o.a.theme
	s := t.Scale
	g := o.heroGeometry(r.Width())
	hero := widget.NewArea(s, r)
	iconRect := hero.Left(widget.HeroIconSize)
	iconRect.Top += (iconRect.Height() - s.Px(widget.HeroIconSize)) / 2
	iconRect.Bottom = iconRect.Top + s.Px(widget.HeroIconSize)
	win32.SetWindowPos(o.heroIcon.HWND(), iconRect)
	hero.Left(heroGap)
	for _, b := range o.heroButtons() {
		r := hero.RightPx(heroButtonWidth(t, b))
		r.Top += (r.Height() - s.Px(widget.ButtonHeight)) / 2
		r.Bottom = r.Top + s.Px(widget.ButtonHeight)
		win32.SetWindowPos(b, r)
		hero.Right(8)
	}
	text := hero.Rest()
	text.Top += (text.Height() - g.textHeight) / 2
	x, y := text.Left, text.Top
	win32.SetWindowPos(o.heroTitle, win32.Rect{Left: x, Top: y, Right: x + g.textWidth, Bottom: y + s.Px(heroTitleHeight)})
	y += s.Px(heroTitleHeight)
	win32.SetWindowPos(o.heroLine, win32.Rect{Left: x, Top: y, Right: x + g.textWidth, Bottom: y + g.lineHeight})
}

// heroGeometry is the layout of the hero's text at a width.
type heroGeometry struct {
	textWidth, textHeight int32
	lineHeight            int32
	height                int32
}

// heroGeometry lays the hero's text out at width pixels: the line wraps
// when it does not fit, and the hero grows with it.
func (o *createPage) heroGeometry(width int32) heroGeometry {
	t := o.a.theme
	s := t.Scale
	g := heroGeometry{textWidth: width - s.Px(widget.HeroIconSize+heroGap+12)}
	for _, b := range o.heroButtons() {
		g.textWidth -= heroButtonWidth(t, b) + s.Px(8)
	}
	g.textWidth = max(g.textWidth, s.Px(200))
	line := win32.Text(o.heroLine)
	g.lineHeight = max(t.Fonts.MeasureWrapped(line, widget.TextSmall, g.textWidth), s.Px(heroLineHeight))
	g.textHeight = s.Px(heroTitleHeight) + g.lineHeight
	g.height = max(s.Px(heroHeight), g.textHeight+s.Px(8))
	return g
}

// heroButtons are the hero's shown buttons, from the right.
func (o *createPage) heroButtons() []win32.HWND {
	var out []win32.HWND
	for _, b := range []win32.HWND{o.heroSecondary, o.heroPrimary} {
		if win32.IsWindowVisible(b) {
			out = append(out, b)
		}
	}
	return out
}

func heroButtonWidth(t *widget.Theme, b win32.HWND) int32 {
	w, _ := t.Fonts.Measure(win32.Text(b), widget.TextBody)
	return max(w+t.Scale.Px(buttonPadding), t.Scale.Px(minButtonWidth))
}
