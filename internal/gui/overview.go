package gui

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"fmt"
	"slices"
	"time"
)

// Control IDs of the Overview page.
const (
	idHeroPrimary = 401 + iota
	idHeroSecondary
	idHeroLink
	idCardLinks // idCardLinks+i is the link of card i
)

// Sizes of the Overview page, in DIPs.
const (
	heroHeight        = 64
	heroTitleHeight   = 28
	heroLineHeight    = 20
	heroGap           = 14
	buttonPadding     = 32
	minButtonWidth    = 120
	cardRowHeight     = 24
	rowGap            = 4
	cardHeadingHeight = 22
	barHeight         = 8
	nameWidth         = 96
	dateWidth         = 120
	nextWidth         = 80
	iconWidth         = 22
	linkPadding       = 12
)

// overviewPage is the Overview (spec 5): the hero and four cards.
type overviewPage struct {
	a     *app
	panel *widget.Panel
	view  view.Overview
	acts  actions

	heroIcon                   *widget.Icon
	heroTitle, heroLine        win32.HWND
	heroLink                   win32.HWND
	heroPrimary, heroSecondary win32.HWND

	folders, storage, last, keys *card

	// run replaces the hero while a backup runs and shows its result;
	// resultOf is the run whose result it shows.
	run      *runCard
	resultOf *flow.Run
	// folderStatus are the state labels of the Folders card while a backup
	// runs; folderSig is the shape they were built for.
	folderStatus map[string]win32.HWND
	folderSig    string
}

func newOverviewPage(a *app) (*overviewPage, error) {
	t := a.theme
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageOverview, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		return nil, err
	}
	o := &overviewPage{a: a, panel: panel, acts: actions{}}
	panel.OnCommand = o.command
	if o.heroIcon, err = widget.NewIcon(t, panel.HWND(), t.Palette.Surface, widget.TextIcon); err != nil {
		return nil, err
	}
	// Creation order is the tab order.
	o.heroTitle = panel.Label("", widget.TextHero, t.Palette.Text)
	o.heroLine = panel.Label("", widget.TextSmall, t.Palette.TextSecondary)
	o.heroLink = o.acts.link(panel, view.Button{Text: " ", Enabled: true}, idHeroLink)
	o.heroPrimary = o.acts.button(panel, view.Button{Text: " "}, idHeroPrimary, true)
	o.heroSecondary = o.acts.button(panel, view.Button{Text: " "}, idHeroSecondary, false)
	if o.run, err = newRunCard(a, panel.HWND()); err != nil {
		return nil, err
	}
	for i, c := range []**card{&o.folders, &o.storage, &o.last, &o.keys} {
		if *c, err = newCard(t, panel.HWND(), uint16(idCardLinks+i), o.acts); err != nil {
			return nil, err
		}
		(*c).panel.OnCommand = o.command
	}
	return o, nil
}

func (o *overviewPage) command(id, code uint16) {
	if action, ok := o.acts[id]; ok && (code == win32.BN_CLICKED || code == 0) {
		o.a.do(action)
	}
}

// update shows the current snapshot and the operation, if any.
func (o *overviewPage) update() {
	o.view = view.OverviewOf(o.a.snapshot, o.a.opts.Config, time.Now())
	t := o.a.theme
	h := o.view.Hero
	fore, circle := heroColors(t.Palette, h.Tone)
	o.heroIcon.Set(glyphOf(h.Glyph), fore, circle, h.Title)
	win32.SetText(o.heroTitle, h.Title)
	win32.SetText(o.heroLine, h.Line)
	o.setButton(o.heroLink, h.Link, idHeroLink, true)
	o.setButton(o.heroPrimary, h.Primary, idHeroPrimary, true)
	if h.Secondary != nil {
		o.setButton(o.heroSecondary, *h.Secondary, idHeroSecondary, true)
	} else {
		o.setButton(o.heroSecondary, view.Button{}, idHeroSecondary, false)
	}
	o.showRun()
	o.fillFolders()
	o.fillStorage()
	o.fillLastBackup()
	o.fillKeys()
	o.layout()
}

// backupRun returns the operation the Overview shows: running, or
// finished with a result card; nil otherwise. A restore shows once it has
// started (spec RW-9); before, the restore wizard is its plan.
func (o *overviewPage) backupRun() *flow.Run {
	r := o.a.machine.Current()
	if r == nil || (r.Op == flow.OpRestore && r.Started.IsZero()) {
		return nil
	}
	return r
}

// showRun shows the run card in place of the hero while a backup runs or
// its result is shown (spec OV-7).
func (o *overviewPage) showRun() {
	r := o.backupRun()
	switch {
	case r != nil && o.a.machine.Busy():
		o.resultOf = nil
		o.run.showProgress(view.ProgressCardOf(r, time.Now()))
	case r != nil && r.Stage == flow.StageFinished:
		if o.resultOf != r {
			if c := view.ResultCardOf(r); c != nil {
				o.run.showResult(*c)
				o.resultOf = r
			} else {
				o.run.hide()
			}
		}
	default:
		o.resultOf = nil
		o.run.hide()
	}
	heroShown := o.run.mode == runHidden
	win32.SetVisible(o.heroIcon.HWND(), heroShown)
	for _, h := range []win32.HWND{o.heroTitle, o.heroLine} {
		win32.SetVisible(h, heroShown)
	}
	for _, h := range []win32.HWND{o.heroLink, o.heroPrimary, o.heroSecondary} {
		if !heroShown {
			setShown(h, false)
		}
	}
}

// updateRun shows a progress report: the run card and the Folders card
// change in place.
func (o *overviewPage) updateRun() {
	if o.run.mode != runProgress {
		o.update()
		return
	}
	o.showRun()
	if !o.updateFolderStates() {
		o.fillFolders()
		o.layout()
	}
}

func (o *overviewPage) fillFolders() {
	t := o.a.theme
	v := o.view.Folders
	c := o.folders
	c.reset()
	c.heading(v.Title, &v.Link)
	states := o.runStates()
	o.folderStatus = map[string]win32.HWND{}
	o.folderSig = folderSig(states)
	for _, row := range v.Rows {
		name := c.label(row.Name, widget.TextBody, t.Palette.Text)
		if state, ok := states[row.Name]; ok {
			icon := cell{dip: iconWidth}
			if state.Glyph != view.GlyphNone {
				if i, err := widget.NewIcon(t, c.panel.HWND(), t.Palette.Surface, widget.TextIconSmall); err == nil {
					c.panel.Adopt(i.HWND())
					i.Set(glyphOf(state.Glyph), toneColor(t.Palette, state.Tone), widget.NoCircle, "")
					icon.hwnd = i.HWND()
				}
			}
			b := c.badge(state.Badge)
			status := c.label(state.Text, widget.TextSmall, toneColor(t.Palette, state.Tone))
			o.folderStatus[row.Name] = status
			c.row(cardRowHeight, icon, cell{hwnd: name, dip: nameWidth}, cell{hwnd: b.HWND(), px: b.Width(), height: 18}, cell{hwnd: status, fill: true})
			continue
		}
		if row.Problem != "" {
			c.row(cardRowHeight, cell{hwnd: name, dip: nameWidth}, cell{hwnd: c.label(row.Problem, widget.TextSmall, toneColor(t.Palette, row.Tone)), fill: true})
			continue
		}
		cells := []cell{{hwnd: name, fill: true}, {hwnd: c.label(row.Date, widget.TextSmall, toneColor(t.Palette, row.Tone)), dip: dateWidth}}
		if row.Badge != nil {
			b := c.badge(*row.Badge)
			cells = append(cells, cell{hwnd: b.HWND(), px: b.Width(), height: 18})
		}
		cells = append(cells, cell{hwnd: c.label(row.Next, widget.TextCaption, t.Palette.TextSecondary), dip: nextWidth})
		c.row(cardRowHeight, cells...)
	}
}

// runStates are the folders' states while a backup runs (spec BR-4).
func (o *overviewPage) runStates() map[string]view.FolderProgress {
	if r := o.backupRun(); r != nil && o.a.machine.Busy() {
		return view.RunFolders(r)
	}
	return nil
}

// updateFolderStates changes the state texts in place; it returns false
// when the rows must be built again (an icon appeared, the run ended).
func (o *overviewPage) updateFolderStates() bool {
	states := o.runStates()
	if states == nil || folderSig(states) != o.folderSig {
		return false
	}
	t := o.a.theme
	for name, s := range states {
		if h, ok := o.folderStatus[name]; ok {
			win32.SetText(h, s.Text)
			o.folders.panel.SetColor(h, toneColor(t.Palette, s.Tone))
		}
	}
	return true
}

// folderSig describes the shape of the folder rows: which folder has which
// icon.
func folderSig(states map[string]view.FolderProgress) string {
	if states == nil {
		return ""
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	slices.Sort(names)
	sig := ""
	for _, name := range names {
		sig += fmt.Sprintf("%s:%d:%s;", name, states[name].Glyph, states[name].Badge.Text)
	}
	return sig
}

func (o *overviewPage) fillStorage() {
	t := o.a.theme
	v := o.view.Storage
	c := o.storage
	c.reset()
	c.row(cardHeadingHeight,
		cell{hwnd: c.panel.PathLabel(v.Path, widget.TextStrong, t.Palette.Text), fill: true},
		cell{hwnd: c.label(v.Used, widget.TextSmall, t.Palette.TextSecondary), px: measure(t, v.Used, widget.TextSmall)})
	if len(v.Segments) == 0 {
		return
	}
	bar, err := widget.NewBar(t, c.panel.HWND(), t.Palette.Surface, t.Palette.SurfaceAlt)
	if err == nil {
		c.panel.Adopt(bar.HWND())
		var segments []widget.Segment
		for _, s := range v.Segments {
			if s.Kind != view.SegmentFree {
				segments = append(segments, widget.Segment{Fraction: s.Fraction, Color: segmentColor(t.Palette, s.Kind)})
			}
		}
		bar.Set(segments, v.Used)
		c.row(barHeight, cell{hwnd: bar.HWND(), fill: true})
	}
	var legend []cell
	for _, s := range v.Segments {
		legend = append(legend, cell{hwnd: c.label(s.Text, widget.TextCaption, t.Palette.TextSecondary), fill: true})
	}
	c.row(cardRowHeight, legend...)
	if v.Estimate != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Estimate, widget.TextSmall, t.Palette.TextSecondary), fill: true})
	}
}

func (o *overviewPage) fillLastBackup() {
	t := o.a.theme
	v := o.view.LastBackup
	c := o.last
	c.reset()
	c.heading(v.Title, &v.Link)
	line := []cell{}
	if v.Glyph != view.GlyphNone {
		icon, err := widget.NewIcon(t, c.panel.HWND(), t.Palette.Surface, widget.TextIconSmall)
		if err == nil {
			c.panel.Adopt(icon.HWND())
			icon.Set(glyphOf(v.Glyph), toneColor(t.Palette, v.Tone), widget.NoCircle, "")
			line = append(line, cell{hwnd: icon.HWND(), dip: iconWidth})
		}
	}
	line = append(line, cell{hwnd: c.label(v.Line, widget.TextBody, t.Palette.Text), fill: true})
	c.row(cardRowHeight, line...)
	for _, row := range v.Rows {
		b := c.badge(row.Badge)
		c.row(cardRowHeight,
			cell{dip: iconWidth},
			cell{hwnd: c.label(row.Name, widget.TextSmall, t.Palette.Text), dip: nameWidth},
			cell{hwnd: b.HWND(), px: b.Width(), height: 18},
			cell{hwnd: c.label(row.Based, widget.TextSmall, toneColor(t.Palette, row.Tone)), fill: true})
	}
}

func (o *overviewPage) fillKeys() {
	t := o.a.theme
	v := o.view.Keys
	c := o.keys
	c.reset()
	c.heading(v.Title, nil)
	if v.Methods != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Methods, widget.TextBody, t.Palette.Text), fill: true})
	}
	if v.Details != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Details, widget.TextSmall, t.Palette.TextSecondary), fill: true})
	}
	if v.Note != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Note, widget.TextSmall, toneColor(t.Palette, v.Tone)), fill: true})
	}
	if v.YubiKey != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.YubiKey, widget.TextSmall, toneColor(t.Palette, v.YubiKeyTone)), fill: true})
	}
}

// layout places the hero and the cards.
func (o *overviewPage) layout() {
	t := o.a.theme
	s := t.Scale
	area := widget.NewArea(s, win32.ClientRect(o.panel.HWND()))
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)

	if o.run.mode != runHidden {
		o.run.place(area.TopPx(o.run.height(area.Rest().Width())))
	} else {
		o.layoutHero(area.Top(heroHeight))
	}

	area.Top(widget.CardGap)
	top := area.Columns(widget.CardGap, 5, 4)
	h1 := max(o.folders.height(), o.storage.height())
	o.folders.place(top[0].Top(h1))
	o.storage.place(top[1].Top(h1))
	area.Top(h1 + widget.CardGap)
	bottom := area.Columns(widget.CardGap, 5, 4)
	h2 := max(o.last.height(), o.keys.height())
	o.last.place(bottom[0].Top(h2))
	o.keys.place(bottom[1].Top(h2))
}

// restyle applies new fonts after a DPI change.
func (o *overviewPage) restyle() {
	o.panel.Restyle()
	for _, c := range []*card{o.folders, o.storage, o.last, o.keys, o.run.card} {
		c.panel.Restyle()
	}
	o.update()
}

// focus puts the keyboard focus on the run card or the hero's primary
// action.
func (o *overviewPage) focus() {
	if o.run.mode != runHidden {
		o.run.focus()
		return
	}
	if win32.IsEnabled(o.heroPrimary) {
		win32.SetFocus(o.heroPrimary)
	}
}

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
	icon   win32.HWND
	dip    int32
	px     int32
	fill   bool
	height int32
}

func newCard(t *widget.Theme, parent win32.HWND, linkID uint16, acts actions) (*card, error) {
	p, err := widget.NewPanel(t, parent, 0, widget.PanelStyle{Back: t.Palette.Surface, Outer: t.Palette.Surface, Card: true})
	if err != nil {
		return nil, err
	}
	return &card{theme: t, panel: p, linkID: linkID, acts: acts}, nil
}

func (c *card) reset() {
	c.panel.Clear()
	c.rows = nil
}

func (c *card) label(text string, style widget.TextStyle, color widget.Color) win32.HWND {
	return c.panel.Label(text, style, color)
}

func (c *card) badge(b view.Badge) *widget.Badge {
	badge, err := widget.NewBadge(c.theme, c.panel.HWND(), c.theme.Palette.Surface)
	if err != nil {
		return &widget.Badge{}
	}
	c.panel.Adopt(badge.HWND())
	fore, back := badgeColors(c.theme.Palette, b.Kind)
	badge.Set(b.Text, fore, back, b.Name)
	return badge
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
			fills++
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
			w = fillW
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
		if c.hwnd != 0 {
			win32.SetWindowPos(c.hwnd, cr)
		}
		x += w + gap
	}
}

// Colors of the view's tones and kinds.

func toneColor(p widget.Palette, t view.Tone) widget.Color {
	switch t {
	case view.ToneSuccess:
		return p.Success
	case view.ToneWarning:
		return p.Warning
	case view.ToneError:
		return p.Error
	case view.ToneInfo:
		return p.AccentText
	case view.ToneSecondary:
		return p.TextSecondary
	}
	return p.Text
}

func heroColors(p widget.Palette, t view.Tone) (fore, circle widget.Color) {
	switch t {
	case view.ToneSuccess:
		return p.Success, p.SuccessBack
	case view.ToneWarning:
		return p.Warning, p.WarningBack
	case view.ToneError:
		return p.Error, p.ErrorBack
	}
	return p.Neutral, p.NeutralBack
}

func badgeColors(p widget.Palette, k view.BadgeKind) (fore, back widget.Color) {
	if k == view.BadgeDiff {
		return p.Diff, p.DiffBack
	}
	return p.Full, p.FullBack
}

func segmentColor(p widget.Palette, k view.SegmentKind) widget.Color {
	if k == view.SegmentBackups {
		return p.BarBackups
	}
	return p.BarOther
}

func glyphOf(g view.Glyph) widget.Glyph {
	switch g {
	case view.GlyphCheck:
		return widget.GlyphCheck
	case view.GlyphWarning:
		return widget.GlyphWarning
	case view.GlyphError:
		return widget.GlyphError
	case view.GlyphInfo:
		return widget.GlyphInfo
	case view.GlyphFolder:
		return widget.GlyphFolder
	case view.GlyphDrive:
		return widget.GlyphDrive
	case view.GlyphKey:
		return widget.GlyphKey
	}
	return widget.GlyphShield
}

// measure returns the width text needs in style, with a little room.
func measure(t *widget.Theme, text string, style widget.TextStyle) int32 {
	w, _ := t.Fonts.Measure(text, style)
	return w + t.Scale.Px(4)
}

// layoutHero places the hero in r.
func (o *overviewPage) layoutHero(r win32.Rect) {
	t := o.a.theme
	s := t.Scale
	hero := widget.NewArea(s, r)
	iconRect := hero.Left(widget.HeroIconSize)
	iconRect.Top += (iconRect.Height() - s.Px(widget.HeroIconSize)) / 2
	iconRect.Bottom = iconRect.Top + s.Px(widget.HeroIconSize)
	win32.SetWindowPos(o.heroIcon.HWND(), iconRect)
	hero.Left(heroGap)
	for _, b := range []win32.HWND{o.heroSecondary, o.heroPrimary} {
		if !win32.IsWindowVisible(b) {
			continue
		}
		w, _ := t.Fonts.Measure(win32.Text(b), widget.TextBody)
		width := max(w+s.Px(buttonPadding), s.Px(minButtonWidth))
		r := hero.RightPx(width)
		r.Top += (r.Height() - s.Px(widget.ButtonHeight)) / 2
		r.Bottom = r.Top + s.Px(widget.ButtonHeight)
		win32.SetWindowPos(b, r)
		hero.Right(8)
	}
	text := widget.NewArea(s, hero.Rest())
	text.Inset(0, (heroHeight-heroTitleHeight-heroLineHeight)/2, 12, 0)
	win32.SetWindowPos(o.heroTitle, text.Top(heroTitleHeight))
	lineRow := text.Top(heroLineHeight)
	lineW, _ := t.Fonts.Measure(win32.Text(o.heroLine), widget.TextSmall)
	linkW, _ := t.Fonts.Measure(o.view.Hero.Link.Text, widget.TextSmall)
	linkW += s.Px(linkPadding)
	lineW = min(lineW+s.Px(4), max(lineRow.Width()-linkW, 0))
	line := lineRow
	line.Right = line.Left + lineW
	win32.SetWindowPos(o.heroLine, line)
	link := lineRow
	link.Left = line.Right + s.Px(8)
	link.Right = min(link.Left+linkW, lineRow.Right)
	win32.SetWindowPos(o.heroLink, link)
}

// setButton shows b on the existing control h, or hides it.
func (o *overviewPage) setButton(h win32.HWND, b view.Button, id uint16, shown bool) {
	shown = shown && b.Text != ""
	if h == o.heroLink {
		win32.SetText(h, "<a>"+b.Text+"</a>")
	} else {
		win32.SetText(h, b.Text)
	}
	win32.SetVisible(h, shown)
	win32.Enable(h, shown && b.Enabled)
	o.acts[id] = b.Action
	win32.Invalidate(h)
}
