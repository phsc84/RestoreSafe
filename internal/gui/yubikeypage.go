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
	idYubiKeySignIn
	idYubiKeyAuthenticator
	idYubiKeyDetails
	idYubiKeyCopy
	idYubiKeyText
)

// Sizes of the YubiKey page, in DIPs.
const (
	checkLabelWidth   = 110
	guideIndent       = 16
	detailsTextHeight = 260
	expanderHeight    = 28
)

// yubiKeyPage is the YubiKey page (GUI spec 10.2): the hero, the Checks
// card, the YubiKey help and the technical details.
type yubiKeyPage struct {
	a     *app
	panel *widget.Panel
	view  view.YubiKeyPage
	acts  actions

	title, checkAgain win32.HWND
	hero              *hero

	checks, guide, details *card
	// The heading of the details opens and closes them; it and their rich
	// edit outlive the card's rebuilds.
	detailsHead *widget.Expander
	text        win32.HWND
	builtWidth  int32
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
	// Creation order is the tab order.
	p.title = panel.Label(view.Navigation()[view.PageYubiKey], widget.TextTitle, t.Palette.Text)
	p.checkAgain = p.acts.button(panel, view.Button{Text: " "}, idYubiKeyCheck, false)
	if p.hero, err = newHero(t, panel, p.acts, idYubiKeyTest); err != nil {
		return nil, err
	}
	for _, c := range []**card{&p.checks, &p.guide, &p.details} {
		if *c, err = newCard(t, panel.HWND(), 0, p.acts); err != nil {
			return nil, err
		}
		(*c).panel.OnCommand = p.command
	}
	if p.detailsHead, err = widget.NewExpander(t, p.details.panel.HWND(), t.Palette.Surface, idYubiKeyDetails); err != nil {
		return nil, err
	}
	p.detailsHead.OnToggle = func(bool) { p.update() }
	p.text, err = win32.CreateWindow(0, win32.MSFTEDIT_CLASS, "",
		// No word wrap: the details are in columns; long paths scroll.
		win32.WS_CHILD|win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_HSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL|win32.ES_AUTOHSCROLL,
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

	check := v.CheckAgain
	check.Enabled = check.Enabled && !a.reloading
	p.setButton(p.checkAgain, check, idYubiKeyCheck)

	p.hero.set(v.Hero)
	p.setButton(p.hero.buttons[0], v.Hero.Primary, idYubiKeyTest)

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

// fillChecks shows a line per check: the icon, the label and the text on
// one baseline, at the top of a line whose text wraps; a note goes under
// the text.
func (p *yubiKeyPage) fillChecks() {
	t := p.a.theme
	pal := t.Palette
	c := p.checks
	c.reset(contentWidth(t, p.panel.HWND()))
	c.heading(p.view.ChecksTitle, nil)
	line := lineHeight(t, widget.TextBody)
	// The text and its note start past the icon and the label.
	textIndent := int32(iconWidth + cellGap + checkLabelWidth)
	for _, l := range p.view.Checks {
		color := toneColor(pal, l.Tone)
		if l.Tone == view.ToneNeutral {
			color = pal.Text
		}
		icon := cell{dip: iconWidth, height: line, top: true}
		if l.Glyph != view.GlyphNone {
			if i, err := widget.NewIcon(t, c.panel.HWND(), pal.Surface, widget.TextIconSmall); err == nil {
				c.panel.Adopt(i.HWND())
				i.Set(glyphOf(l.Glyph), color, widget.NoCircle, "")
				icon.hwnd = i.HWND()
			}
		}
		c.row(c.textHeight(l.Text, widget.TextBody, c.textWidth(textIndent), line),
			icon,
			cell{hwnd: c.panel.Paragraph(l.Label, widget.TextBody, pal.TextSecondary), dip: checkLabelWidth},
			cell{hwnd: c.panel.Paragraph(l.Text, widget.TextBody, color), fill: true})
		if l.Note != "" {
			c.para(l.Note, widget.TextSmall, pal.TextSecondary, textIndent)
		}
	}
}

// guideIDs are the control IDs of the help's buttons, in order.
var guideIDs = []uint16{idYubiKeySignIn, idYubiKeyAuthenticator}

// fillGuide shows the YubiKey help, always open: per part its title, its
// points as a list, and its button, with a gap before the next part.
func (p *yubiKeyPage) fillGuide() {
	t := p.a.theme
	pal := t.Palette
	g := p.view.Guide
	c := p.guide
	c.reset(contentWidth(t, p.panel.HWND()))
	c.heading(g.Title, nil)
	for i, s := range g.Steps {
		if i > 0 {
			c.row(rowGap * 2)
		}
		c.row(cardRowHeight, cell{hwnd: c.label(s.Title, widget.TextStrong, pal.Text), fill: true})
		for _, point := range s.Points {
			p.bullet(c, point)
		}
		if s.Action != nil && i < len(guideIDs) {
			h := c.acts.button(c.panel, *s.Action, guideIDs[i], false)
			c.row(widget.ButtonHeight+4, cell{dip: guideIndent}, cell{hwnd: h, px: buttonWidth(t, h), height: widget.ButtonHeight})
		}
	}
}

// bullet adds a point of a list: a bullet, and the text that wraps under
// itself, not under the bullet.
func (p *yubiKeyPage) bullet(c *card, text string) {
	t := p.a.theme
	pal := t.Palette
	c.row(c.textHeight(text, widget.TextBody, c.textWidth(guideIndent), lineHeight(t, widget.TextBody)),
		cell{hwnd: c.panel.Paragraph("•", widget.TextBody, pal.TextSecondary), dip: guideIndent},
		cell{hwnd: c.panel.Paragraph(text, widget.TextBody, pal.Text), fill: true})
}

// fillDetails shows the technical details: the heading opens and closes
// them; Copy details shows only while they are open.
func (p *yubiKeyPage) fillDetails() {
	t := p.a.theme
	c := p.details
	c.reset(0)
	open := p.detailsHead.Open()
	p.detailsHead.Set(p.view.DetailsTitle, open)
	cells := []cell{{hwnd: p.detailsHead.HWND(), px: p.detailsHead.Width()}, {fill: true}}
	if open {
		copyButton := c.acts.button(c.panel, p.view.CopyDetails, idYubiKeyCopy, false)
		cells = append(cells, cell{hwnd: copyButton, px: buttonWidth(t, copyButton), height: widget.ButtonHeight})
	}
	c.row(expanderHeight, cells...)
	text := strings.ReplaceAll(p.view.Details, "\n", "\r\n")
	if win32.Text(p.text) != text {
		win32.SetText(p.text, text)
	}
	win32.SetAccessibleName(p.text, p.view.DetailsTitle)
	setShown(p.text, open)
	if open {
		c.row(detailsTextHeight, cell{hwnd: p.text, fill: true})
	}
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
	heroH := p.hero.geometry(contentWidth(t, p.panel.HWND())).height
	total := 2*s.Px(widget.ContentPaddingY) + s.Px(pageTitleHeight) + s.Px(8) + heroH
	for _, c := range []*card{p.checks, p.guide, p.details} {
		total += s.Px(widget.CardGap) + s.Px(c.height())
	}
	p.panel.SetScroll(total)
	if win32.ClientRect(p.panel.HWND()).Width() != client.Width() {
		// A scroll bar came or went: the cards and the wrapped lines need
		// the new width, or they reach under the scroll bar.
		p.update()
		return
	}

	page := client
	page.Top -= p.panel.ScrollOffset()
	page.Bottom = page.Top + max(total, client.Height())
	area := widget.NewArea(s, page)
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)
	placeTitle(t, p.title, p.checkAgain, area.Top(pageTitleHeight))
	area.Top(8)
	p.hero.place(area.TopPx(heroH))
	for _, c := range []*card{p.checks, p.guide, p.details} {
		area.Top(widget.CardGap)
		c.place(area.Top(c.height()))
	}
}

// focus puts the keyboard focus on the hero's action, or on Check again.
func (p *yubiKeyPage) focus() {
	if test := p.hero.buttons[0]; win32.IsWindowVisible(test) && win32.IsEnabled(test) {
		win32.SetFocus(test)
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
