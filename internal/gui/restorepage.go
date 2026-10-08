package gui

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"strings"
	"time"
)

// Control IDs of the Restore backup page.
const (
	idBackupsFilter = 601 + iota
	idBackupsList
	idBackupsRestore
	idBackupsVerify
	_ // 605 to 608 were the log pane's buttons; the IDs after them keep their values
	_
	_
	_
	_ // was the empty page's Back up now button
	idBackupsRefresh
	idLineButtons = 640 // idLineButtons+i is the button of line i
)

// Context menu items of the list.
const (
	menuRestore = 1 + iota
	menuVerify
	menuShowLog
	menuCopyName
	menuOpenFolder
)

// Sizes of the Restore backup page, in DIPs.
const (
	pageTitleHeight  = 32
	filterWidth      = 220
	filterDropHeight = 300
	actionBarHeight  = 36
	minListHeight    = 160
	lineGap          = 4
)

// Column widths of the list, in DIPs; the status column takes the rest.
var backupColumnWidths = []int32{150, 72, 110, 80, 76}

// restorePage is the Restore backup page (GUI spec 7): the runs and their sets, the
// selection's actions; each run's header links to its log.
type restorePage struct {
	a     *app
	panel *widget.Panel
	acts  actions
	view  view.RestorePage

	title, filter, refresh win32.HWND
	// run is the restore or verification at the top of the page: its
	// progress, then its result.
	run         *runCard
	lines       *widget.Panel
	linesStack  *stack
	linesSig    string
	linesHeight int32 // pixels

	list    win32.HWND
	listSig string
	// rows maps the param of a list item to its group and row; row -1 is
	// the placeholder of a run without sets.
	rows []rowRef
	// lastGroups are the runs of the list's groups, by group ID.
	lastGroups []naming.BackupID
	// shownCollapsed is how each group was shown: a group whose state
	// differs at the next rebuild was changed by the user.
	shownCollapsed []bool

	barText, restore, verify win32.HWND
	bar                      view.ActionBar

	folder    string
	selRun    naming.BackupID
	selSet    string
	collapsed map[naming.BackupID]bool
}

func newRestorePage(a *app) (*restorePage, error) {
	t := a.theme
	pal := t.Palette
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageRestore, widget.PanelStyle{Back: pal.Surface})
	if err != nil {
		return nil, err
	}
	b := &restorePage{a: a, panel: panel, acts: actions{}, collapsed: map[naming.BackupID]bool{}}
	b.title = panel.Label(view.RestorePageOf(nil, nil, nil, "", time.Now()).Title, widget.TextTitle, pal.Text)
	b.filter = b.child("COMBOBOX", win32.WS_TABSTOP|win32.WS_VSCROLL|win32.CBS_DROPDOWNLIST, idBackupsFilter)
	win32.SetAccessibleName(b.filter, view.FilterName)
	b.refresh = b.acts.button(panel, view.Button{Text: " "}, idBackupsRefresh, false)
	if b.run, err = newRunCard(a, panel.HWND()); err != nil {
		return nil, err
	}
	b.run.decorate = func(c *view.ResultCard) {
		view.AddProblemHint(c, a.snapshot, a.opts.Config, time.Now(), true)
	}
	if b.lines, err = widget.NewPanel(t, panel.HWND(), 0, widget.PanelStyle{Back: pal.Surface, Outer: pal.Surface, Card: true}); err != nil {
		return nil, err
	}
	b.lines.OnCommand = b.command
	b.list = b.child(win32.WC_LISTVIEW, win32.WS_TABSTOP|win32.WS_BORDER|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_SHOWSELALWAYS|win32.LVS_NOSORTHEADER, idBackupsList)
	win32.ListSetup(b.list)
	widget.StyleListHeader(t, b.list)
	win32.ListEnableInfoTips(b.list)
	for i, c := range view.RestorePageOf(nil, nil, nil, "", time.Now()).Columns {
		win32.ListInsertColumn(b.list, i, c, t.Scale.Px(150), i == 3)
	}
	win32.SetAccessibleName(b.list, view.BackupsListName)
	b.barText = panel.Label("", widget.TextBody, pal.Text)
	b.restore = panel.Button(" ", idBackupsRestore)
	b.verify = panel.Button(" ", idBackupsVerify)
	panel.OnCommand = b.command
	panel.OnNotify = b.notify
	panel.OnScroll = b.layout
	b.restyle()
	return b, nil
}

// child creates a control on the page with the body font.
func (b *restorePage) child(class string, style uint32, id uintptr) win32.HWND {
	h, err := win32.CreateWindow(0, class, "", win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, b.panel.HWND(), id)
	if err != nil {
		return 0
	}
	b.panel.Adopt(h)
	return h
}

// restyle applies the theme's fonts, after creation and DPI changes.
func (b *restorePage) restyle() {
	t := b.a.theme
	b.panel.Restyle()
	b.run.card.panel.Restyle()
	b.lines.Restyle()
	for _, h := range []win32.HWND{b.filter, b.list} {
		win32.SetFont(h, t.Fonts.Get(widget.TextBody))
	}
	for i, w := range backupColumnWidths {
		win32.ListSetColumnWidth(b.list, i, t.Scale.Px(w))
	}
	b.linesSig = ""
}

// update shows the current snapshot and operation.
func (b *restorePage) update() {
	a := b.a
	b.view = view.RestorePageOf(a.snapshot, a.opts.Config, a.machine.Current(), b.folder, time.Now())
	v := b.view

	var filters []string
	for _, f := range v.Filters {
		filters = append(filters, f.Text)
	}
	if strings.Join(filters, "\x00") != b.filterSig() {
		win32.ComboSet(b.filter, filters, v.Filter)
	}

	b.showRun()
	b.lines.Show(v.Retention != nil || len(v.Lines) > 0)
	b.fillLines()
	b.fillList()
	b.updateBar()
	b.layout()
}

// opRun returns the restore or verification the page shows: running, or
// finished with a result card; nil otherwise. A restore shows once it has
// started (GUI spec RW-9); before, the Restore window is its plan.
func (b *restorePage) opRun() *flow.Run {
	r := b.a.machine.Current()
	if r == nil || r.Op == flow.OpBackup || (r.Op == flow.OpRestore && r.Started.IsZero()) {
		return nil
	}
	return r
}

// showRun shows the restore or verification on the run card; it reports
// whether the card appeared or went away.
func (b *restorePage) showRun() bool {
	b.a.showRefresh(b.refresh, b.view.Refresh, b.acts, idBackupsRefresh)
	return b.run.follow(b.opRun(), b.a.machine.Busy())
}

// updateRun shows a progress report: in full while the page is shown,
// otherwise only the run card.
func (b *restorePage) updateRun() {
	if b.a.page == view.PageRestore {
		b.update()
		return
	}
	if b.showRun() {
		b.layout()
	}
}

func (b *restorePage) filterSig() string {
	n := int(win32.SendMessage(b.filter, 0x0146, 0, 0)) // CB_GETCOUNT
	if n <= 0 {
		return ""
	}
	parts := make([]string, 0, n)
	for _, f := range b.view.Filters {
		parts = append(parts, f.Text)
	}
	if len(parts) != n {
		return ""
	}
	return strings.Join(parts, "\x00")
}

// fillLines rebuilds the retention and problem lines when they changed.
func (b *restorePage) fillLines() {
	v := b.view
	lines := v.Lines
	if v.Retention != nil {
		lines = append([]view.InfoLine{*v.Retention}, lines...)
	}
	var sig strings.Builder
	for _, l := range lines {
		sig.WriteString(l.Text)
		sig.WriteByte(0)
	}
	if sig.String() == b.linesSig {
		return
	}
	b.linesSig = sig.String()
	t := b.a.theme
	s := t.Scale
	p := b.lines
	p.Clear()
	width := win32.ClientRect(b.panel.HWND()).Width() - s.Px(2*widget.ContentPaddingX+2*widget.CardPadding)
	st := newStack(t, p, max(width, s.Px(200)))
	for i, l := range lines {
		if i > 0 {
			st.gap(lineGap)
		}
		color := toneColor(t.Palette, l.Tone)
		if l.Button == nil {
			st.para(l.Text, widget.TextSmall, color, l.Glyph)
			continue
		}
		bw, _ := t.Fonts.Measure(l.Button.Text, widget.TextBody)
		bw += s.Px(buttonPadding)
		inner := newStack(t, p, st.width-bw-s.Px(8))
		inner.para(l.Text, widget.TextSmall, color, l.Glyph)
		btn := b.acts.button(p, *l.Button, uint16(idLineButtons+i), false)
		h := max(inner.height(), s.Px(widget.ButtonHeight))
		st.items = append(st.items, stackItem{height: h, place: func(r win32.Rect) {
			inner.place(r.Left, r.Top)
			win32.SetWindowPos(btn, win32.Rect{Left: r.Right - bw, Top: r.Top, Right: r.Right, Bottom: r.Top + s.Px(widget.ButtonHeight)})
		}})
	}
	b.linesHeight = st.height() + 2*s.Px(widget.CardPadding)
	b.linesStack = st
}

// updateBar shows the selection and its actions.
func (b *restorePage) updateBar() {
	b.bar = view.SelectionOf(b.view, b.selRun, b.selSet)
	win32.SetText(b.barText, b.bar.Text)
	for _, x := range []struct {
		h  win32.HWND
		bt view.Button
		id uint16
	}{{b.restore, b.bar.Restore, idBackupsRestore}, {b.verify, b.bar.Verify, idBackupsVerify}} {
		win32.SetText(x.h, x.bt.Text)
		win32.Enable(x.h, x.bt.Enabled && !b.a.machine.Busy())
		b.acts[x.id] = x.bt.Action
	}
	// The selected run's header is painted selected (customDraw); the list
	// can't show a group selected itself.
	win32.Invalidate(b.list)
}

func (b *restorePage) command(id, code uint16) {
	a := b.a
	switch {
	case id == idBackupsFilter && code == win32.CBN_SELCHANGE:
		if i := win32.ComboSelected(b.filter); i >= 0 && i < len(b.view.Filters) {
			b.folder = b.view.Filters[i].Folder
			b.update()
		}
	case code == win32.BN_CLICKED || code == 0:
		if action, ok := b.acts[id]; ok {
			a.do(action)
		}
	}
}

// chosen returns the selected sets that can be restored or verified.
func (b *restorePage) chosen() []naming.BackupEntry {
	if b.a.snapshot == nil {
		return nil
	}
	var out []naming.BackupEntry
	for _, name := range b.bar.Sets {
		for _, info := range b.a.snapshot.Sets {
			if info.Entry.String() == name {
				out = append(out, info.Entry)
			}
		}
	}
	return out
}

// focus puts the keyboard focus on the run card or the list.
func (b *restorePage) focus() {
	if b.run.mode != runHidden {
		b.run.focus()
		return
	}
	win32.SetFocus(b.list)
}
