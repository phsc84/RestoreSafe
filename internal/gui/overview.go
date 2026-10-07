package gui

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"strings"
	"time"
)

// Control IDs of the Overview page.
const (
	idHeroPrimary = 401 + iota
	idHeroSecondary
	idCardLinks // idCardLinks+i is the link of card i
	idHeroTitle = 409
	idRefresh   = 410
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
	iconWidth         = 22
	linkPadding       = 12
	refreshHeight     = 22
)

// overviewPage is the Overview (GUI spec 5): the title, the hero and three
// cards in one column.
type overviewPage struct {
	a     *app
	panel *widget.Panel
	view  view.Overview
	acts  actions

	title                      win32.HWND
	heroIcon                   *widget.Icon
	heroTitle, heroLine        win32.HWND
	heroPrimary, heroSecondary win32.HWND
	refresh                    win32.HWND

	folders, storage, keys *card

	// run replaces the hero while a backup runs and shows its result.
	run *runCard
	// busy is whether an operation was busy at the last update: a restore
	// or verification disables the hero's Back up now.
	busy bool
	// folderTable lists the folders in the Folders card.
	folderTable *table
	// sizer is the splitter below the Folders card that sets the table's
	// height.
	sizer *tableSizer
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
	o.title = panel.Label(view.OverviewOf(nil, nil, time.Now()).Title, widget.TextTitle, t.Palette.Text)
	// Creation order is the tab order.
	o.heroTitle = panel.Label("", widget.TextHero, t.Palette.Text)
	win32.SetControlID(o.heroTitle, idHeroTitle)
	o.heroLine = panel.Paragraph("", widget.TextSmall, t.Palette.TextSecondary)
	o.heroPrimary = o.acts.button(panel, view.Button{Text: " "}, idHeroPrimary, true)
	o.heroSecondary = o.acts.button(panel, view.Button{Text: " "}, idHeroSecondary, false)
	o.refresh = o.acts.button(panel, view.Button{Text: " "}, idRefresh, false)
	if o.run, err = newRunCard(a, panel.HWND()); err != nil {
		return nil, err
	}
	o.run.decorate = func(c *view.ResultCard) {
		view.AddProblemHint(c, a.snapshot, a.opts.Config, time.Now(), false)
	}
	for i, c := range []**card{&o.folders, &o.storage, &o.keys} {
		if *c, err = newCard(t, panel.HWND(), uint16(idCardLinks+i), o.acts); err != nil {
			return nil, err
		}
		(*c).panel.OnCommand = o.command
	}
	// More than five folders scroll within the table (GUI spec OV-3).
	o.folderTable = newTable(t, o.folders.panel.HWND(), 0)
	o.folders.panel.OnNotify = func(hdr *win32.NMHdr) uintptr {
		r, _ := o.folderTable.notify(hdr)
		return r
	}
	if o.sizer, err = newTableSizer(t, panel, o.folders, o.folderTable, o.layout); err != nil {
		return nil, err
	}
	panel.OnScroll = o.layout
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
	if o.a.reloadErr != nil && o.a.snapshot != nil {
		o.view.Hero = view.ReloadErrorHero(o.a.reloadErr)
	}
	h := o.view.Hero
	fore, circle := heroColors(t.Palette, h.Tone)
	o.heroIcon.Set(glyphOf(h.Glyph), fore, circle, h.Title)
	win32.SetText(o.heroTitle, h.Title)
	// A path in the title keeps its end (the folder) when shortened.
	style := win32.Style(o.heroTitle) &^ (win32.SS_ENDELLIPSIS | win32.SS_PATHELLIPSIS)
	if strings.Contains(h.Title, ``) {
		win32.SetStyle(o.heroTitle, style|win32.SS_PATHELLIPSIS)
	} else {
		win32.SetStyle(o.heroTitle, style|win32.SS_ENDELLIPSIS)
	}
	win32.SetText(o.heroLine, h.Line)
	// While a restore or verification runs, no backup can start.
	o.busy = o.a.machine.Busy()
	idle := func(b view.Button) view.Button {
		b.Enabled = b.Enabled && !(o.busy && b.Action == view.ActionBackUp)
		return b
	}
	o.setButton(o.heroPrimary, idle(h.Primary), idHeroPrimary, true)
	if h.Secondary != nil {
		o.setButton(o.heroSecondary, idle(*h.Secondary), idHeroSecondary, true)
	} else {
		o.setButton(o.heroSecondary, view.Button{}, idHeroSecondary, false)
	}
	o.showRun()
	o.fillFolders()
	o.fillStorage()
	o.fillKeys()
	o.layout()
}

// backupRun returns the backup the page shows: running, or finished with a
// result card; nil otherwise. Restores and verifications show on Restore
// backup.
func (o *overviewPage) backupRun() *flow.Run {
	r := o.a.machine.Current()
	if r == nil || r.Op != flow.OpBackup {
		return nil
	}
	return r
}

// showRun shows the run card in place of the hero while a backup runs or
// its result is shown (GUI spec OV-7).
func (o *overviewPage) showRun() {
	o.run.follow(o.backupRun(), o.a.machine.Busy())
	heroShown := o.run.mode == runHidden
	win32.SetVisible(o.heroIcon.HWND(), heroShown)
	for _, h := range []win32.HWND{o.heroTitle, o.heroLine} {
		win32.SetVisible(h, heroShown)
	}
	for _, h := range []win32.HWND{o.heroPrimary, o.heroSecondary} {
		if !heroShown {
			setShown(h, false)
		}
	}
	o.a.showRefresh(o.refresh, o.view.Refresh, o.acts, idRefresh)
}

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

// updateRun shows a progress report: the run card and the Folders card
// change in place.
func (o *overviewPage) updateRun() {
	if o.backupRun() == nil && o.run.mode == runHidden && o.busy == o.a.machine.Busy() {
		return // a restore or verification still running, or none
	}
	if o.run.mode != runProgress {
		o.update()
		return
	}
	o.showRun()
	o.folderTable.set(o.view.Folders.Table(o.runStates()))
}

// fillFolders shows the folders in the table and, below it, a note on a
// failed or cancelled backup.
func (o *overviewPage) fillFolders() {
	t := o.a.theme
	v := o.view.Folders
	c := o.folders
	c.reset()
	c.heading(v.Title, nil)
	o.folderTable.set(v.Table(o.runStates()))
	c.row(o.sizer.rowHeight(), o.folderTable.cell())
	if v.Note != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Note, widget.TextSmall, toneColor(t.Palette, v.NoteTone)), fill: true})
	}
}

// runStates are the folders' states while a backup runs (GUI spec BR-4).
func (o *overviewPage) runStates() map[string]view.FolderProgress {
	if r := o.backupRun(); r != nil && o.a.machine.Busy() {
		return view.RunFolders(r)
	}
	return nil
}

func (o *overviewPage) fillStorage() {
	t := o.a.theme
	v := o.view.Storage
	c := o.storage
	c.reset()
	c.heading(v.Title, nil)
	c.row(cardRowHeight,
		cell{hwnd: c.panel.PathLabel(v.Path, widget.TextBody, t.Palette.Text), fill: true},
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
		legend = append(legend, cell{hwnd: c.tip(c.label(s.Text, widget.TextCaption, t.Palette.TextSecondary), s.Tip), fill: true})
	}
	c.row(cardRowHeight, legend...)
	if v.Estimate != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Estimate, widget.TextSmall, t.Palette.TextSecondary), fill: true})
	}
}

func (o *overviewPage) fillKeys() {
	t := o.a.theme
	v := o.view.Keys
	c := o.keys
	c.reset()
	c.heading(v.Title, nil)
	// The YubiKey status sits at the right of the methods line, like the
	// used space on the storage card.
	yubi := func() cell {
		return cell{hwnd: c.label(v.YubiKey, widget.TextSmall, toneColor(t.Palette, v.YubiKeyTone)), px: measure(t, v.YubiKey, widget.TextSmall)}
	}
	switch {
	case v.Methods != "" && v.YubiKey != "":
		c.row(cardRowHeight, cell{hwnd: c.label(v.Methods, widget.TextBody, t.Palette.Text), fill: true}, yubi())
	case v.Methods != "":
		c.row(cardRowHeight, cell{hwnd: c.label(v.Methods, widget.TextBody, t.Palette.Text), fill: true})
	}
	if v.Details != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Details, widget.TextSmall, t.Palette.TextSecondary), fill: true})
	}
	if v.Note != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Note, widget.TextSmall, toneColor(t.Palette, v.Tone)), fill: true})
	}
	if v.Methods == "" && v.YubiKey != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.YubiKey, widget.TextSmall, toneColor(t.Palette, v.YubiKeyTone)), fill: true})
	}
}

// layout places the title, the hero or the run card below it, like on
// Restore backup, and the cards. The page scrolls when they don't fit.
func (o *overviewPage) layout() {
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
	placeTitle(t, o.title, o.refresh, area.Top(backupsTitleHeight))
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
func (o *overviewPage) contentHeight(width int32) int32 {
	s := o.a.theme.Scale
	inner := width - 2*s.Px(widget.ContentPaddingX)
	h := 2*s.Px(widget.ContentPaddingY) + s.Px(backupsTitleHeight) + s.Px(8)
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

// restyle applies new fonts after a DPI change.
func (o *overviewPage) restyle() {
	o.panel.Restyle()
	o.folderTable.restyle()
	for _, c := range []*card{o.folders, o.storage, o.keys, o.run.card} {
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
func (o *overviewPage) heroGeometry(width int32) heroGeometry {
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
func (o *overviewPage) heroButtons() []win32.HWND {
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

// setButton shows b on the existing control h, or hides it.
func (o *overviewPage) setButton(h win32.HWND, b view.Button, id uint16, shown bool) {
	shown = shown && b.Text != ""
	win32.SetText(h, b.Text)
	win32.SetVisible(h, shown)
	win32.Enable(h, shown && b.Enabled)
	o.acts[id] = b.Action
	win32.Invalidate(h)
}

// share returns the weight of a fill cell.
func (c cell) share() int32 {
	return max(c.weight, 1)
}
