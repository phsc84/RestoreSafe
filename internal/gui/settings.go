package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// Control IDs of the Settings page.
const (
	idSettingsEdit = 801 + iota
	idSettingsReload
	idSettingsOpen
	idSettingsMore
	idSettingsAdd
)

// Sizes of the Settings page, in DIPs.
const (
	settingsTitleHeight = 32
	settingLabelWidth   = 190
)

// settingsPage is the Settings page (spec 10): the configuration in use,
// read-only, with Edit config.yaml and Reload.
type settingsPage struct {
	a     *app
	panel *widget.Panel
	acts  actions
	view  view.SettingsPage
	title win32.HWND

	config, folders, backupDir *card
	differential, retention    *card
	checks, keys, logging      *card
	// foldersTable lists the folders in the Folders card.
	foldersTable *table
	// showArgon2 expands the key derivation values.
	showArgon2 bool
	// builtWidth is the page width the rows were measured for; a new width
	// measures them again.
	builtWidth int32
}

func newSettingsPage(a *app) (*settingsPage, error) {
	t := a.theme
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageSettings, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		return nil, err
	}
	sp := &settingsPage{a: a, panel: panel, acts: actions{}}
	sp.title = panel.Label(view.SettingsOf(a.opts.Config, a.opts.ConfigPath, a.backupDir, nil, nil, false, "").Title, widget.TextTitle, t.Palette.Text)
	for _, c := range []**card{&sp.config, &sp.folders, &sp.backupDir, &sp.differential, &sp.retention, &sp.checks, &sp.keys, &sp.logging} {
		if *c, err = newCard(t, panel.HWND(), 0, sp.acts); err != nil {
			return nil, err
		}
		(*c).panel.OnCommand = sp.command
	}
	sp.foldersTable = newTable(t, sp.folders.panel.HWND(), 0)
	sp.folders.panel.OnNotify = func(hdr *win32.NMHdr) uintptr {
		r, _ := sp.foldersTable.notify(hdr)
		return r
	}
	panel.OnCommand = sp.command
	panel.OnScroll = sp.layout
	return sp, nil
}

func (sp *settingsPage) cards() []*card {
	return []*card{sp.config, sp.folders, sp.backupDir, sp.differential, sp.retention, sp.checks, sp.keys, sp.logging}
}

func (sp *settingsPage) command(id, code uint16) {
	if code != win32.BN_CLICKED && code != 0 {
		return
	}
	if id == idSettingsMore {
		sp.showArgon2 = !sp.showArgon2
		sp.update()
		return
	}
	if action, ok := sp.acts[id]; ok {
		sp.a.do(action)
	}
}

// update shows the configuration in use and the state the check found.
func (sp *settingsPage) update() {
	a := sp.a
	sp.view = view.SettingsOf(a.opts.Config, a.opts.ConfigPath, a.backupDir, a.snapshot, a.reloadErr, a.machine.Busy() || a.reloading, a.addedCopy)
	v := sp.view
	t := a.theme
	pal := t.Palette

	c := sp.config
	c.reset()
	c.heading(v.ConfigTitle, nil)
	c.row(cardRowHeight+4,
		cell{hwnd: c.panel.PathLabel(v.ConfigPath, widget.TextBody, pal.Text), fill: true},
		sp.button(c, v.Edit, idSettingsEdit),
		sp.button(c, v.Reload, idSettingsReload))
	sp.para(c, v.ConfigNote, widget.TextSmall, pal.TextSecondary)
	if v.ConfigError != "" {
		sp.para(c, v.ConfigError, widget.TextSmall, pal.Error)
	}
	if v.Added != "" {
		sp.para(c, v.Added, widget.TextSmall, toneColor(pal, view.ToneSuccess))
	}
	if v.Missing != "" {
		sp.para(c, v.Missing, widget.TextSmall, toneColor(pal, view.ToneInfo))
		c.row(cardRowHeight+4, sp.button(c, v.AddMissing, idSettingsAdd))
	}

	c = sp.folders
	c.reset()
	c.heading(v.FoldersTitle, nil)
	sp.foldersTable.set(v.FoldersTable())
	c.row(sp.foldersTable.height(), sp.foldersTable.cell())
	sp.rows(c, v.FolderRows)

	c = sp.backupDir
	c.reset()
	c.heading(v.BackupDirTitle, nil)
	st := v.BackupDirState
	c.row(cardRowHeight+4,
		cell{hwnd: c.panel.PathLabel(v.BackupDir, widget.TextBody, pal.Text), fill: true},
		sp.status(c, st.Value, st.Tone, st.Glyph),
		sp.button(c, v.Open, idSettingsOpen))
	sp.rows(c, v.BackupDirRows)

	sp.fillCard(sp.differential, v.Differential)
	sp.fillCard(sp.retention, v.Retention)
	sp.fillCard(sp.checks, v.Checks)
	sp.fillCard(sp.keys, v.Keys)
	sp.fillCard(sp.logging, v.Logging)
	sp.builtWidth = win32.ClientRect(sp.panel.HWND()).Width()
	sp.layout()
}

// fillCard shows a card of settings.
func (sp *settingsPage) fillCard(c *card, v view.SettingsCard) {
	t := sp.a.theme
	pal := t.Palette
	c.reset()
	c.heading(v.Title, nil)
	sp.rows(c, v.Rows)
	if v.MoreLink != "" {
		w, _ := t.Fonts.Measure(v.MoreLink, widget.TextSmall)
		c.row(cardRowHeight, cell{hwnd: c.panel.Link(v.MoreLink, idSettingsMore), px: w + t.Scale.Px(linkPadding)})
		if sp.showArgon2 {
			sp.rows(c, v.More)
			sp.para(c, v.MoreNote, widget.TextSmall, pal.TextSecondary)
		}
	}
	if v.Note != "" {
		sp.para(c, v.Note, widget.TextSmall, toneColor(pal, v.NoteTone))
	}
}

// rows adds settings as label and value; both wrap within the card.
func (sp *settingsPage) rows(c *card, rows []view.Setting) {
	t := sp.a.theme
	s := t.Scale
	pal := t.Palette
	labelDip := int32(settingLabelWidth)
	if sp.cardWidth(c) < s.Px(2*settingLabelWidth+80) {
		labelDip = settingLabelWidth * 2 / 3
	}
	valueW := sp.cardWidth(c) - 2*s.Px(widget.CardPadding) - s.Px(labelDip) - s.Px(8)
	for _, r := range rows {
		value := pal.Text
		if r.Tone != view.ToneNeutral {
			value = toneColor(pal, r.Tone)
		}
		h := max(
			t.Fonts.MeasureWrapped(r.Label, widget.TextSmall, s.Px(labelDip)),
			t.Fonts.MeasureWrapped(r.Value, widget.TextBody, max(valueW, s.Px(80))),
			s.Px(cardRowHeight))
		c.row(h*96/int32(s)+1,
			cell{hwnd: c.panel.Paragraph(r.Label, widget.TextSmall, pal.TextSecondary), dip: labelDip},
			cell{hwnd: c.tip(c.panel.Paragraph(r.Value, widget.TextBody, value), view.KeyTip(r.Key)), fill: true})
	}
}

// para adds text that wraps over the card's width.
func (sp *settingsPage) para(c *card, text string, style widget.TextStyle, color widget.Color) {
	t := sp.a.theme
	s := t.Scale
	h := c.panel.Paragraph(text, style, color)
	width := sp.cardWidth(c) - 2*s.Px(widget.CardPadding)
	px := max(t.Fonts.MeasureWrapped(text, style, max(width, s.Px(200))), s.Px(stackLineHeight))
	c.row(px*96/int32(s)+1, cell{hwnd: h, fill: true})
}

// cardWidth returns the width a card gets, in pixels: the page's content
// width, or half of it for the cards in pairs.
func (sp *settingsPage) cardWidth(c *card) int32 {
	s := sp.a.theme.Scale
	w := win32.ClientRect(sp.panel.HWND()).Width() - 2*s.Px(widget.ContentPaddingX)
	switch c {
	case sp.differential, sp.retention, sp.checks, sp.keys:
		return (w - s.Px(widget.CardGap)) / 2
	}
	return w
}

// status creates a status cell: an icon and its text.
func (sp *settingsPage) status(c *card, text string, tone view.Tone, glyph view.Glyph) cell {
	t := sp.a.theme
	pal := t.Palette
	color := toneColor(pal, tone)
	label := c.label(text, widget.TextSmall, color)
	w, _ := t.Fonts.Measure(text, widget.TextSmall)
	w += t.Scale.Px(6)
	if glyph == view.GlyphNone {
		return cell{hwnd: label, px: w}
	}
	// The icon and the text share the cell: the icon is placed by the row,
	// the text after it.
	icon, err := widget.NewIcon(t, c.panel.HWND(), pal.Surface, widget.TextIconSmall)
	if err != nil {
		return cell{hwnd: label, px: w}
	}
	c.panel.Adopt(icon.HWND())
	icon.Set(glyphOf(glyph), color, widget.NoCircle, "")
	return cell{hwnd: label, px: w + t.Scale.Px(iconWidth), icon: icon.HWND()}
}

// button creates a button cell for b.
func (sp *settingsPage) button(c *card, b view.Button, id uint16) cell {
	h := c.acts.button(c.panel, b, id, false)
	return cell{hwnd: h, px: buttonWidth(sp.a.theme, h), height: widget.ButtonHeight}
}

// layout places the cards at the page's scroll position.
func (sp *settingsPage) layout() {
	if w := win32.ClientRect(sp.panel.HWND()).Width(); w != sp.builtWidth && w > 0 {
		sp.update() // the wrapped rows need the new width
		return
	}
	t := sp.a.theme
	s := t.Scale
	client := win32.ClientRect(sp.panel.HWND())
	gap := s.Px(widget.CardGap)
	padX, padY := s.Px(widget.ContentPaddingX), s.Px(widget.ContentPaddingY)

	// The content's height decides whether the page scrolls.
	total := 2*padY + s.Px(settingsTitleHeight)
	for _, c := range []*card{sp.config, sp.folders, sp.backupDir, sp.logging} {
		total += gap + s.Px(c.height())
	}
	for _, pair := range [][2]*card{{sp.differential, sp.retention}, {sp.checks, sp.keys}} {
		total += gap + s.Px(max(pair[0].height(), pair[1].height()))
	}
	sp.panel.SetScroll(total)
	client = win32.ClientRect(sp.panel.HWND())

	y := padY - sp.panel.ScrollOffset()
	left, right := padX, client.Width()-padX
	win32.SetWindowPos(sp.title, win32.Rect{Left: left, Top: y, Right: right, Bottom: y + s.Px(settingsTitleHeight)})
	y += s.Px(settingsTitleHeight)
	full := func(c *card) {
		y += gap
		h := s.Px(c.height())
		c.place(win32.Rect{Left: left, Top: y, Right: right, Bottom: y + h})
		y += h
	}
	pair := func(a, b *card) {
		y += gap
		h := s.Px(max(a.height(), b.height()))
		mid := left + (right-left-gap)/2
		a.place(win32.Rect{Left: left, Top: y, Right: mid, Bottom: y + h})
		b.place(win32.Rect{Left: mid + gap, Top: y, Right: right, Bottom: y + h})
		y += h
	}
	full(sp.config)
	full(sp.folders)
	full(sp.backupDir)
	pair(sp.differential, sp.retention)
	pair(sp.checks, sp.keys)
	full(sp.logging)
}

// restyle applies new fonts after a DPI change.
func (sp *settingsPage) restyle() {
	sp.panel.Restyle()
	sp.foldersTable.restyle()
	for _, c := range sp.cards() {
		c.panel.Restyle()
	}
	sp.update()
}
