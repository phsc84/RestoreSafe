package gui

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"strings"
	"time"
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

func newCreatePage(a *app) (*createPage, error) {
	t := a.theme
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageCreate, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		return nil, err
	}
	o := &createPage{a: a, panel: panel, acts: actions{}}
	panel.OnCommand = o.command
	if o.heroIcon, err = widget.NewIcon(t, panel.HWND(), t.Palette.Surface, widget.TextIcon); err != nil {
		return nil, err
	}
	o.title = panel.Label(view.CreatePageOf(nil, nil, time.Now()).Title, widget.TextTitle, t.Palette.Text)
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

func (o *createPage) command(id, code uint16) {
	if action, ok := o.acts[id]; ok && (code == win32.BN_CLICKED || code == 0) {
		o.a.do(action)
	}
}

// update shows the current snapshot and the operation, if any.
func (o *createPage) update() {
	o.view = view.CreatePageOf(o.a.snapshot, o.a.opts.Config, time.Now())
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
	c.reset()
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

func (o *createPage) fillKeys() {
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
	if win32.IsEnabled(o.heroPrimary) {
		win32.SetFocus(o.heroPrimary)
	}
}

// Colors of the view's tones and kinds.

// setButton shows b on the existing control h, or hides it.
func (o *createPage) setButton(h win32.HWND, b view.Button, id uint16, shown bool) {
	shown = shown && b.Text != ""
	win32.SetText(h, b.Text)
	win32.SetVisible(h, shown)
	win32.Enable(h, shown && b.Enabled)
	o.acts[id] = b.Action
	win32.Invalidate(h)
}
