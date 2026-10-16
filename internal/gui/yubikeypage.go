package gui

import (
	"strings"
	"time"

	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// Control IDs of the YubiKey page.
const (
	idYubiKeyCheck = 901 + iota
	idYubiKeyTest
	idYubiKeyGuide
	idYubiKeySignIn
	idYubiKeyAuthenticator
	idYubiKeyDetails
	idYubiKeyCopy
	idYubiKeyText
)

// Sizes of the YubiKey page, in DIPs.
const (
	checkLabelWidth   = 110
	guideIndent       = 28
	detailsTextHeight = 260
)

// yubiKeyPage is the YubiKey page (GUI spec 10.2): the hero, the Checks
// card, the setup guide and the technical details.
type yubiKeyPage struct {
	a     *app
	panel *widget.Panel
	view  view.YubiKeyPage
	acts  actions

	title, checkAgain   win32.HWND
	heroIcon            *widget.Icon
	heroTitle, heroLine win32.HWND
	heroPrimary         win32.HWND

	checks, guide, details *card
	// text is the read-only rich edit of the technical details; it belongs
	// to the details card but outlives its rebuilds.
	text win32.HWND
	// showGuide and showDetails are what the user expanded.
	showGuide, showDetails bool
	builtWidth             int32
}

func newYubiKeyPage(a *app) (*yubiKeyPage, error) {
	t := a.theme
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageYubiKey, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		return nil, err
	}
	p := &yubiKeyPage{a: a, panel: panel, acts: actions{}}
	panel.OnCommand = p.command
	panel.OnScroll = p.layout
	if p.heroIcon, err = widget.NewIcon(t, panel.HWND(), t.Palette.Surface, widget.TextIcon); err != nil {
		return nil, err
	}
	// Creation order is the tab order.
	p.title = panel.Label(view.Navigation()[view.PageYubiKey], widget.TextTitle, t.Palette.Text)
	p.checkAgain = p.acts.button(panel, view.Button{Text: " "}, idYubiKeyCheck, false)
	p.heroTitle = panel.Label("", widget.TextHero, t.Palette.Text)
	p.heroLine = panel.Paragraph("", widget.TextSmall, t.Palette.TextSecondary)
	p.heroPrimary = p.acts.button(panel, view.Button{Text: " "}, idYubiKeyTest, true)
	for _, c := range []**card{&p.checks, &p.guide, &p.details} {
		if *c, err = newCard(t, panel.HWND(), 0, p.acts); err != nil {
			return nil, err
		}
		(*c).panel.OnCommand = p.command
	}
	p.text, err = win32.CreateWindow(0, win32.MSFTEDIT_CLASS, "",
		win32.WS_CHILD|win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL,
		0, 0, 0, 0, p.details.panel.HWND(), idYubiKeyText)
	if err != nil {
		return nil, err
	}
	win32.SendMessage(p.text, win32.EM_SETTEXTMODE, win32.TM_PLAINTEXT, 0)
	win32.SendMessage(p.text, win32.EM_SETBKGNDCOLOR, 0, uintptr(win32.SysColor(win32.COLOR_WINDOW)))
	win32.SetFont(p.text, a.monoFont)
	return p, nil
}

func (p *yubiKeyPage) command(id, code uint16) {
	if code != win32.BN_CLICKED && code != 0 {
		return
	}
	switch id {
	case idYubiKeyGuide:
		p.showGuide = !p.showGuide
		p.update()
		return
	case idYubiKeyDetails:
		p.showDetails = !p.showDetails
		p.update()
		return
	}
	if action, ok := p.acts[id]; ok {
		p.a.do(action)
	}
}

// update shows the snapshot, the last diagnosis and test, and whether a
// test or an operation runs.
func (p *yubiKeyPage) update() {
	a := p.a
	p.view = view.YubiKeyPageOf(a.snapshot, a.yubiKeyView(), time.Now())
	v := p.view
	t := a.theme

	check := v.CheckAgain
	check.Enabled = check.Enabled && !a.reloading
	p.setButton(p.checkAgain, check, idYubiKeyCheck)

	h := v.Hero
	fore, circle := heroColors(t.Palette, h.Tone)
	p.heroIcon.Set(glyphOf(h.Glyph), fore, circle, h.Title)
	win32.SetText(p.heroTitle, h.Title)
	win32.SetText(p.heroLine, h.Line)
	p.setButton(p.heroPrimary, h.Primary, idYubiKeyTest)

	p.fillChecks()
	p.fillGuide()
	p.fillDetails()
	p.builtWidth = win32.ClientRect(p.panel.HWND()).Width()
	p.layout()
	win32.RedrawAll(p.panel.HWND())
}

// setButton shows b on h, or hides h when b has no text.
func (p *yubiKeyPage) setButton(h win32.HWND, b view.Button, id uint16) {
	shown := b.Text != ""
	win32.SetText(h, b.Text)
	win32.SetVisible(h, shown)
	win32.Enable(h, shown && b.Enabled)
	p.acts[id] = b.Action
	if b.Reason != "" {
		p.panel.Tip(h, b.Reason)
	}
	win32.Invalidate(h)
}

func (p *yubiKeyPage) fillChecks() {
	t := p.a.theme
	s := t.Scale
	c := p.checks
	c.reset()
	c.heading(p.view.ChecksTitle, nil)
	// The note starts under the text: past the icon, the label and their
	// gap (para adds the gap after the indent).
	noteIndent := int32(iconWidth + checkLabelWidth + 8)
	for _, l := range p.view.Checks {
		color := toneColor(t.Palette, l.Tone)
		if l.Tone == view.ToneNeutral {
			color = t.Palette.Text
		}
		icon := cell{dip: iconWidth}
		if l.Glyph != view.GlyphNone {
			if i, err := widget.NewIcon(t, c.panel.HWND(), t.Palette.Surface, widget.TextIconSmall); err == nil {
				c.panel.Adopt(i.HWND())
				i.Set(glyphOf(l.Glyph), color, widget.NoCircle, "")
				icon.hwnd = i.HWND()
			}
		}
		text := c.panel.Paragraph(l.Text, widget.TextBody, color)
		width := p.cardWidth() - 2*s.Px(widget.CardPadding) - s.Px(noteIndent+8)
		c.row(p.dips(max(t.Fonts.MeasureWrapped(l.Text, widget.TextBody, max(width, s.Px(80))), s.Px(cardRowHeight))),
			icon,
			cell{hwnd: c.label(l.Label, widget.TextSmall, t.Palette.TextSecondary), dip: checkLabelWidth},
			cell{hwnd: text, fill: true})
		if l.Note != "" {
			p.para(c, l.Note, widget.TextSmall, t.Palette.TextSecondary, noteIndent)
		}
	}
}

// guideIDs are the control IDs of the guide steps' buttons, in order.
var guideIDs = []uint16{idYubiKeySignIn, idYubiKeyAuthenticator}

func (p *yubiKeyPage) fillGuide() {
	t := p.a.theme
	pal := t.Palette
	g := p.view.Guide
	c := p.guide
	c.reset()
	if !g.Expanded && !p.showGuide {
		c.row(cardRowHeight, p.linkCell(c, view.Button{Text: g.Collapsed, Enabled: true}, idYubiKeyGuide))
		return
	}
	c.heading(g.Title, nil)
	buttons := 0
	for _, s := range g.Steps {
		c.row(cardRowHeight,
			cell{hwnd: c.label(s.Number, widget.TextStrong, pal.AccentText), dip: guideIndent},
			cell{hwnd: c.label(s.Title, widget.TextStrong, pal.Text), fill: true})
		p.para(c, s.Text, widget.TextSmall, pal.TextSecondary, guideIndent)
		if s.Action != nil && buttons < len(guideIDs) {
			h := c.acts.button(c.panel, *s.Action, guideIDs[buttons], false)
			buttons++
			c.row(widget.ButtonHeight+4, cell{dip: guideIndent}, cell{hwnd: h, px: buttonWidth(t, h), height: widget.ButtonHeight})
		}
	}
}

// fillDetails shows the technical details as an expander: the button opens
// and closes them; Copy details shows only while they are open.
func (p *yubiKeyPage) fillDetails() {
	t := p.a.theme
	c := p.details
	c.reset()
	chevron := " ▾"
	if p.showDetails {
		chevron = " ▴"
	}
	toggle := c.acts.button(c.panel, view.Button{Text: p.view.DetailsTitle + chevron, Enabled: true}, idYubiKeyDetails, false)
	win32.SetAccessibleName(toggle, p.view.DetailsTitle)
	cells := []cell{{hwnd: toggle, px: buttonWidth(t, toggle), height: widget.ButtonHeight}, {fill: true}}
	if p.showDetails {
		copyButton := c.acts.button(c.panel, p.view.CopyDetails, idYubiKeyCopy, false)
		cells = append(cells, cell{hwnd: copyButton, px: buttonWidth(t, copyButton), height: widget.ButtonHeight})
	}
	c.row(widget.ButtonHeight+4, cells...)
	text := strings.ReplaceAll(p.view.Details, "\n", "\r\n")
	if win32.Text(p.text) != text {
		win32.SetText(p.text, text)
	}
	win32.SetAccessibleName(p.text, p.view.DetailsTitle)
	setShown(p.text, p.showDetails)
	if p.showDetails {
		c.row(detailsTextHeight, cell{hwnd: p.text, fill: true})
	}
}

// linkCell creates a link for b that fits its text.
func (p *yubiKeyPage) linkCell(c *card, b view.Button, id uint16) cell {
	w, _ := p.a.theme.Fonts.Measure(b.Text, widget.TextSmall)
	return cell{hwnd: c.acts.link(c.panel, b, id), px: w + p.a.theme.Scale.Px(linkPadding)}
}

// para adds text that wraps over the card's width, indented by indent DIPs.
func (p *yubiKeyPage) para(c *card, text string, style widget.TextStyle, color widget.Color, indent int32) {
	t := p.a.theme
	s := t.Scale
	h := c.panel.Paragraph(text, style, color)
	width := p.cardWidth() - 2*s.Px(widget.CardPadding)
	if indent > 0 {
		width -= s.Px(indent + 8) // the spacer and the gap after it
	}
	px := max(t.Fonts.MeasureWrapped(text, style, max(width, s.Px(200))), s.Px(stackLineHeight))
	if indent > 0 {
		c.row(p.dips(px), cell{dip: indent}, cell{hwnd: h, fill: true})
		return
	}
	c.row(p.dips(px), cell{hwnd: h, fill: true})
}

// dips converts pixels to DIPs, rounding up.
func (p *yubiKeyPage) dips(px int32) int32 { return px*96/int32(p.a.theme.Scale) + 1 }

// cardWidth returns the width of the cards in pixels.
func (p *yubiKeyPage) cardWidth() int32 {
	s := p.a.theme.Scale
	return win32.ClientRect(p.panel.HWND()).Width() - 2*s.Px(widget.ContentPaddingX)
}

// layout places the title, the hero and the cards; the page scrolls when
// they don't fit.
func (p *yubiKeyPage) layout() {
	if w := win32.ClientRect(p.panel.HWND()).Width(); w != p.builtWidth && w > 0 {
		p.update() // the wrapped lines need the new width
		return
	}
	t := p.a.theme
	s := t.Scale
	client := win32.ClientRect(p.panel.HWND())
	inner := client.Width() - 2*s.Px(widget.ContentPaddingX)
	heroH, textW, lineH := p.heroGeometry(inner)
	total := 2*s.Px(widget.ContentPaddingY) + s.Px(pageTitleHeight) + s.Px(8) + heroH
	for _, c := range []*card{p.checks, p.guide, p.details} {
		total += s.Px(widget.CardGap) + s.Px(c.height())
	}
	p.panel.SetScroll(total)

	page := client
	page.Top -= p.panel.ScrollOffset()
	page.Bottom = page.Top + max(total, client.Height())
	area := widget.NewArea(s, page)
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)
	placeTitle(t, p.title, p.checkAgain, area.Top(pageTitleHeight))
	area.Top(8)

	hero := widget.NewArea(s, area.TopPx(heroH))
	iconRect := hero.Left(widget.HeroIconSize)
	iconRect.Top += (iconRect.Height() - s.Px(widget.HeroIconSize)) / 2
	iconRect.Bottom = iconRect.Top + s.Px(widget.HeroIconSize)
	win32.SetWindowPos(p.heroIcon.HWND(), iconRect)
	hero.Left(heroGap)
	if win32.IsWindowVisible(p.heroPrimary) {
		r := hero.RightPx(heroButtonWidth(t, p.heroPrimary))
		r.Top += (r.Height() - s.Px(widget.ButtonHeight)) / 2
		r.Bottom = r.Top + s.Px(widget.ButtonHeight)
		win32.SetWindowPos(p.heroPrimary, r)
	}
	text := hero.Rest()
	text.Top += (text.Height() - s.Px(heroTitleHeight) - lineH) / 2
	x, y := text.Left, text.Top
	win32.SetWindowPos(p.heroTitle, win32.Rect{Left: x, Top: y, Right: x + textW, Bottom: y + s.Px(heroTitleHeight)})
	y += s.Px(heroTitleHeight)
	win32.SetWindowPos(p.heroLine, win32.Rect{Left: x, Top: y, Right: x + textW, Bottom: y + lineH})

	for _, c := range []*card{p.checks, p.guide, p.details} {
		area.Top(widget.CardGap)
		c.place(area.Top(c.height()))
	}
}

// heroGeometry lays the hero out at width pixels: its height, the width
// of its text and the height of its line, which wraps.
func (p *yubiKeyPage) heroGeometry(width int32) (height, textW, lineH int32) {
	t := p.a.theme
	s := t.Scale
	textW = width - s.Px(widget.HeroIconSize+heroGap+12)
	if win32.IsWindowVisible(p.heroPrimary) {
		textW -= heroButtonWidth(t, p.heroPrimary) + s.Px(8)
	}
	textW = max(textW, s.Px(200))
	lineH = 0
	if line := win32.Text(p.heroLine); line != "" {
		lineH = max(t.Fonts.MeasureWrapped(line, widget.TextSmall, textW), s.Px(heroLineHeight))
	}
	height = max(s.Px(heroHeight), s.Px(heroTitleHeight)+lineH+s.Px(8))
	return height, textW, lineH
}

// focus puts the keyboard focus on the hero's action, or on Check again.
func (p *yubiKeyPage) focus() {
	if win32.IsWindowVisible(p.heroPrimary) && win32.IsEnabled(p.heroPrimary) {
		win32.SetFocus(p.heroPrimary)
		return
	}
	if win32.IsEnabled(p.checkAgain) {
		win32.SetFocus(p.checkAgain)
	}
}

// restyle applies new fonts after a DPI change.
func (p *yubiKeyPage) restyle() {
	p.panel.Restyle()
	for _, c := range []*card{p.checks, p.guide, p.details} {
		c.panel.Restyle()
	}
	win32.SetFont(p.text, p.a.monoFont)
	p.update()
}
