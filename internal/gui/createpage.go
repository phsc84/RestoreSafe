package gui

import (
	"time"

	"github.com/phsc84/restoresafe/internal/gui/flow"
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// Control IDs of the Create backup page.
const (
	idHeroPrimary = 401 + iota
	idHeroSecondary
	idCardLinks // idCardLinks+i is the link of card i
	idHeroTitle = 409
	idRefresh   = 410
)

// Sizes of the Create backup page, in DIPs.
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
	calloutPadding    = 8
	linkPadding       = 12
	refreshHeight     = 22
)

// createPage is the Create backup page (GUI spec 5): the title, the hero and three
// cards in one column.
type createPage struct {
	a     *app
	panel *widget.Panel
	view  view.CreatePage
	acts  actions

	title   win32.HWND
	hero    *hero
	refresh win32.HWND

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
	// builtWidth is the page width the wrapped lines were measured for; a
	// new width measures them again.
	builtWidth int32
}

func newCreatePage(a *app) (*createPage, error) {
	t := a.theme
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageCreate, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		return nil, err
	}
	o := &createPage{a: a, panel: panel, acts: actions{}}
	panel.OnCommand = o.command
	o.title = panel.Label(view.CreatePageOf(nil, nil, time.Now()).Title, widget.TextTitle, t.Palette.Text)
	// Creation order is the tab order.
	if o.hero, err = newHero(t, panel, o.acts, idHeroPrimary, idHeroSecondary); err != nil {
		return nil, err
	}
	win32.SetControlID(o.hero.title, idHeroTitle)
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

func (o *createPage) command(id, code uint16) {
	if action, ok := o.acts[id]; ok && (code == win32.BN_CLICKED || code == 0) {
		o.a.do(action)
	}
}

// update shows the current snapshot and the operation, if any.
func (o *createPage) update() {
	o.view = view.CreatePageOf(o.a.snapshot, o.a.opts.Config, time.Now())
	if o.a.reloadErr != nil && o.a.snapshot != nil {
		o.view.Hero = view.ReloadErrorHero(o.a.reloadErr)
	}
	h := o.view.Hero
	o.hero.set(h)
	// While a restore or verification runs, no backup can start.
	o.busy = o.a.machine.Busy()
	idle := func(b view.Button) view.Button {
		b.Enabled = b.Enabled && !(o.busy && b.Action == view.ActionBackUp)
		return b
	}
	primary, secondary := o.hero.buttons[0], o.hero.buttons[1]
	o.setButton(primary, idle(h.Primary), idHeroPrimary, true)
	if h.Secondary != nil {
		o.setButton(secondary, idle(*h.Secondary), idHeroSecondary, true)
	} else {
		o.setButton(secondary, view.Button{}, idHeroSecondary, false)
	}
	o.showRun()
	o.fillFolders()
	o.fillStorage()
	o.fillKeys()
	o.builtWidth = win32.ClientRect(o.panel.HWND()).Width()
	o.layout()
	// Moving a card copies its old pixels, painted before its labels were
	// rebuilt: the run card taking the hero's place left parts of the old
	// cards showing.
	win32.RedrawAll(o.panel.HWND())
}

// backupRun returns the backup the page shows: running, or finished with a
// result card; nil otherwise. Restores and verifications show on Restore
// backup.
func (o *createPage) backupRun() *flow.Run {
	r := o.a.machine.Current()
	if r == nil || r.Op != flow.OpBackup {
		return nil
	}
	return r
}

// showRun shows the run card in place of the hero while a backup runs or
// its result is shown (GUI spec OV-7).
func (o *createPage) showRun() {
	o.run.follow(o.backupRun(), o.a.machine.Busy())
	o.hero.show(o.run.mode == runHidden)
	o.a.showRefresh(o.refresh, o.view.Refresh, o.acts, idRefresh)
}

// updateRun shows a progress report: the run card and the Folders card
// change in place.
func (o *createPage) updateRun() {
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
func (o *createPage) fillFolders() {
	t := o.a.theme
	v := o.view.Folders
	c := o.folders
	c.reset(contentWidth(t, o.panel.HWND()))
	c.heading(v.Title, nil)
	o.folderTable.set(v.Table(o.runStates()))
	c.row(o.sizer.rowHeight(), o.folderTable.cell())
	if v.Note != "" {
		c.row(cardRowHeight, cell{hwnd: c.label(v.Note, widget.TextSmall, toneColor(t.Palette, v.NoteTone)), fill: true})
	}
}

// runStates are the folders' states while a backup runs (GUI spec BR-4).
func (o *createPage) runStates() map[string]view.FolderProgress {
	if r := o.backupRun(); r != nil && o.a.machine.Busy() {
		return view.RunFolders(r)
	}
	return nil
}

func (o *createPage) fillStorage() {
	t := o.a.theme
	v := o.view.Storage
	c := o.storage
	c.reset(contentWidth(t, o.panel.HWND()))
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

func (o *createPage) fillKeys() {
	t := o.a.theme
	v := o.view.Keys
	c := o.keys
	c.reset(contentWidth(t, o.panel.HWND()))
	// The YubiKey status sits at the right of the methods line, like the
	// used space on the storage card; without keys, at the right of the
	// heading.
	yubi := func() cell {
		return cell{hwnd: c.label(v.YubiKey, widget.TextSmall, toneColor(t.Palette, v.YubiKeyTone)), px: measure(t, v.YubiKey, widget.TextSmall)}
	}
	if v.Methods == "" && v.YubiKey != "" {
		c.row(cardHeadingHeight, cell{hwnd: c.label(v.Title, widget.TextStrong, t.Palette.Text), fill: true}, yubi())
	} else {
		c.heading(v.Title, nil)
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
		c.para(v.Note, widget.TextSmall, toneColor(t.Palette, v.Tone), view.GlyphNone)
	}
	if v.Warning != "" {
		o.callout(c, v.Warning)
	}
}

// callout adds a warning that stands out: a warning icon and small text
// that wraps, in a box of the warning's background color.
func (o *createPage) callout(c *card, text string) {
	t := o.a.theme
	pal := t.Palette
	box, err := widget.NewPanel(t, c.panel.HWND(), 0, widget.PanelStyle{Back: pal.WarningBack, Outer: pal.Surface, Card: true})
	if err != nil {
		c.para(text, widget.TextSmall, pal.Warning, view.GlyphWarning)
		return
	}
	c.panel.Adopt(box.HWND())
	pad := t.Scale.Px(calloutPadding)
	in := newStack(t, box, c.width-2*pad)
	in.para(text, widget.TextSmall, pal.Warning, view.GlyphWarning)
	c.add(in.height()+2*pad, func(r win32.Rect) {
		win32.SetWindowPos(box.HWND(), r)
		in.place(pad, pad, r.Width()-2*pad)
	})
}

// restyle applies new fonts after a DPI change.
func (o *createPage) restyle() {
	o.panel.Restyle()
	o.folderTable.restyle()
	for _, c := range []*card{o.folders, o.storage, o.keys, o.run.card} {
		c.panel.Restyle()
	}
	o.update()
}

// focus puts the keyboard focus on the run card or the hero's primary
// action.
func (o *createPage) focus() {
	if o.run.mode != runHidden {
		o.run.focus()
		return
	}
	if primary := o.hero.buttons[0]; win32.IsEnabled(primary) {
		win32.SetFocus(primary)
	}
}

// setButton shows b on the existing control h, or hides it.
func (o *createPage) setButton(h win32.HWND, b view.Button, id uint16, shown bool) {
	shown = shown && b.Text != ""
	win32.SetText(h, b.Text)
	win32.SetVisible(h, shown)
	win32.Enable(h, shown && b.Enabled)
	o.acts[id] = b.Action
	win32.Invalidate(h)
}

// layout places the title, the hero or the run card below it, like on
// Restore backup, and the cards. The page scrolls when they don't fit.
func (o *createPage) layout() {
	if w := win32.ClientRect(o.panel.HWND()).Width(); w != o.builtWidth && w > 0 {
		o.update() // the wrapped lines need the new width
		return
	}
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
		o.hero.place(area.TopPx(o.hero.geometry(area.Rest().Width()).height))
	}

	for _, c := range []*card{o.folders, o.storage, o.keys} {
		gap := area.Top(widget.CardGap)
		if c == o.storage {
			o.sizer.place(gap)
		}
		c.place(area.TopPx(c.height()))
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
		h += o.hero.geometry(inner).height
	}
	for _, c := range []*card{o.folders, o.storage, o.keys} {
		h += s.Px(widget.CardGap) + c.height()
	}
	return h
}

// measure returns the width text needs in style, with a little room.
func measure(t *widget.Theme, text string, style widget.TextStyle) int32 {
	w, _ := t.Fonts.Measure(text, style)
	return w + t.Scale.Px(4)
}
