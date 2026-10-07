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

type rowRef struct{ group, row int }

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

// fillList rebuilds the list when its runs or sets changed and otherwise
// updates the texts in place, so the selection and scrolling stay.
func (b *restorePage) fillList() {
	v := b.view
	var sig strings.Builder
	for _, g := range v.Groups {
		sig.WriteString(string(g.RunID))
		sig.WriteByte('|')
		sig.WriteString(g.Header)
		sig.WriteByte('|')
		sig.WriteString(g.LogPath)
		sig.WriteByte(0)
		for _, r := range g.Rows {
			sig.WriteString(r.Set)
			sig.WriteByte(0)
		}
	}
	lv := b.list
	if sig.String() == b.listSig {
		for i, ref := range b.rows {
			if ref.row >= 0 {
				r := v.Groups[ref.group].Rows[ref.row]
				win32.ListSetText(lv, b.itemOf(i), 5, r.Status.Text)
			}
		}
		win32.Invalidate(lv)
		return
	}
	// Keep what the user collapsed or expanded: a group whose state differs
	// from how it was shown.
	for gi := range b.groupsShown() {
		if gi < len(b.lastGroups) && gi < len(b.shownCollapsed) {
			if now := win32.ListGroupCollapsed(lv, int32(gi)); now != b.shownCollapsed[gi] {
				b.collapsed[b.lastGroups[gi]] = now
			}
		}
	}
	b.shownCollapsed = nil
	b.listSig = sig.String()
	win32.SendMessage(lv, win32.WM_SETREDRAW, 0, 0)
	win32.ListClear(lv)
	b.rows = nil
	b.lastGroups = nil
	selected := -1
	for gi, g := range v.Groups {
		collapsed, known := b.collapsed[g.RunID]
		if !known || g.RunID == "" {
			collapsed = !g.Expanded
		}
		link := ""
		if g.LogPath != "" {
			link = view.RunLogLink
		}
		win32.ListInsertGroup(lv, int32(gi), g.Header, link, collapsed)
		b.shownCollapsed = append(b.shownCollapsed, collapsed)
		b.lastGroups = append(b.lastGroups, g.RunID)
		if len(g.Rows) == 0 {
			b.rows = append(b.rows, rowRef{gi, -1})
			win32.ListInsertItem(lv, g.Placeholder, int32(gi), uintptr(len(b.rows)))
			continue
		}
		for ri, r := range g.Rows {
			b.rows = append(b.rows, rowRef{gi, ri})
			i := win32.ListInsertItem(lv, r.Folder, int32(gi), uintptr(len(b.rows)))
			for col, text := range []string{r.Badge.Text, r.BasedOn, r.Size, r.Chain, r.Status.Text} {
				win32.ListSetText(lv, i, col+1, text)
			}
			if r.Set == b.selSet && b.selSet != "" {
				selected = i
			}
		}
	}
	win32.SendMessage(lv, win32.WM_SETREDRAW, 1, 0)
	if selected >= 0 {
		win32.ListSelect(lv, selected)
	} else if b.selSet != "" {
		b.selSet = ""
	}
	win32.Invalidate(lv)
}

// groupsShown returns the indexes of the groups in the list.
func (b *restorePage) groupsShown() []int {
	out := make([]int, len(b.lastGroups))
	for i := range out {
		out[i] = i
	}
	return out
}

// itemOf returns the list index of the item with param index i+1.
func (b *restorePage) itemOf(i int) int {
	n := win32.ListItemCount(b.list)
	for item := range n {
		if int(win32.ListParam(b.list, item)) == i+1 {
			return item
		}
	}
	return -1
}

// rowAt returns the group and row of list item i; ok is false for none.
func (b *restorePage) rowAt(item int) (rowRef, bool) {
	if item < 0 {
		return rowRef{}, false
	}
	p := int(win32.ListParam(b.list, item))
	if p < 1 || p > len(b.rows) {
		return rowRef{}, false
	}
	return b.rows[p-1], true
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

// runSelected reports whether group gi is the selected run.
func (b *restorePage) runSelected(gi int) bool {
	return gi >= 0 && gi < len(b.view.Groups) && b.selSet == "" && b.selRun != "" && b.view.Groups[gi].RunID == b.selRun
}

// selectItem makes the run of list item i the selection: a run is restored
// and verified whole, so its sets aren't selected one by one. Incomplete
// sets have no run; such a set is selected itself.
func (b *restorePage) selectItem(item int) {
	ref, ok := b.rowAt(item)
	if !ok {
		return
	}
	g := b.view.Groups[ref.group]
	b.selRun, b.selSet = g.RunID, ""
	if g.RunID != "" {
		// The item keeps the focus, so the arrow keys move on from it.
		win32.ListDeselect(b.list, item)
	} else if ref.row >= 0 {
		b.selSet = g.Rows[ref.row].Set
	}
	b.updateBar()
}

// selectRun makes the run of group gi the selection.
func (b *restorePage) selectRun(gi int) {
	if gi < 0 || gi >= len(b.view.Groups) {
		return
	}
	g := b.view.Groups[gi]
	b.selRun, b.selSet = g.RunID, ""
	win32.ListSelect(b.list, -1)
	b.updateBar()
}

// showRunLog shows the log of group gi in the log window (GUI spec BK-5).
func (b *restorePage) showRunLog(gi int) {
	if gi < 0 || gi >= len(b.view.Groups) {
		return
	}
	g := b.view.Groups[gi]
	b.a.showLog(b.a.hwnd, g.LogPath, g.When)
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

// notify handles the list's notifications.
func (b *restorePage) notify(hdr *win32.NMHdr) uintptr {
	if hdr.HwndFrom != b.list {
		return 0
	}
	switch hdr.Code {
	case win32.LVN_ITEMCHANGED:
		n := win32.ListChangeOf(hdr)
		if n.Changed&win32.LVIF_STATE != 0 && n.NewState&win32.LVIS_SELECTED != 0 && n.OldState&win32.LVIS_SELECTED == 0 {
			b.selectItem(int(n.Item))
		}
		// A click on a group header selects the group's first item and
		// focuses the header; it means the run.
		win32.PostMessage(b.a.hwnd, msgListFocus, 0, 0) //nolint:errcheck
	case win32.NM_CLICK, win32.LVN_KEYDOWN:
		// After the list moved the focus, a group header may have it.
		win32.PostMessage(b.a.hwnd, msgListFocus, 0, 0) //nolint:errcheck
	case win32.NM_DBLCLK:
		if b.bar.Restore.Enabled {
			b.a.do(view.ActionRestore)
		}
	case win32.NM_RCLICK:
		b.contextMenu()
	case win32.LVN_LINKCLICK:
		// The log window runs its own message loop: it opens after the
		// list has finished with the click.
		win32.PostMessage(b.a.hwnd, msgRunLog, uintptr(win32.ListLinkGroup(hdr)), 0) //nolint:errcheck
	case win32.LVN_GETINFOTIP:
		n := win32.ListInfoTipOf(hdr)
		if ref, ok := b.rowAt(int(n.Item)); ok && ref.row >= 0 {
			n.SetText(b.view.Groups[ref.group].Rows[ref.row].Tip)
		}
	case win32.NM_CUSTOMDRAW:
		return b.customDraw(win32.ListDrawOf(hdr))
	}
	return 0
}

// focusChanged selects the run whose group header has the keyboard focus.
func (b *restorePage) focusChanged() {
	g := int(win32.ListFocusedGroup(b.list))
	if g < 0 || g >= len(b.view.Groups) {
		return
	}
	if b.selSet != "" || b.selRun != b.view.Groups[g].RunID {
		b.selectRun(g)
	}
}

// contextMenu offers the actions of the item under the cursor (GUI spec BK-4).
func (b *restorePage) contextMenu() {
	screen := win32.CursorPos()
	item := win32.ListHitItem(b.list, win32.ScreenToClient(b.list, screen))
	if item < 0 {
		return
	}
	win32.ListSelect(b.list, item)
	b.selectItem(item)
	ref, _ := b.rowAt(item)
	logPath := b.view.Groups[ref.group].LogPath
	m := view.RestoreMenu()
	set := ""
	if ref.row >= 0 {
		set = b.view.Groups[ref.group].Rows[ref.row].Set
	}
	busy := b.a.machine.Busy()
	switch win32.ShowMenu(b.a.hwnd, screen, []win32.MenuItem{
		{ID: menuRestore, Text: m.Restore, Disabled: !b.bar.Restore.Enabled || busy},
		{ID: menuVerify, Text: m.Verify, Disabled: !b.bar.Verify.Enabled || busy},
		{ID: menuShowLog, Text: m.ShowLog, Disabled: logPath == ""},
		{},
		{ID: menuCopyName, Text: m.CopyName, Disabled: set == ""},
		{ID: menuOpenFolder, Text: m.OpenFolder},
	}) {
	case menuRestore:
		b.a.do(view.ActionRestore)
	case menuVerify:
		b.a.do(view.ActionVerify)
	case menuShowLog:
		b.showRunLog(ref.group)
	case menuCopyName:
		win32.CopyText(b.a.hwnd, set) //nolint:errcheck
	case menuOpenFolder:
		b.a.do(view.ActionOpenBackupDir)
	}
}

// customDraw colors the status, sets the chain in the monospaced font and
// draws the type badges over their cells.
func (b *restorePage) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := b.a.theme
	pal := t.Palette
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
		if cd.ItemType == win32.LVCDI_GROUP {
			// A group (ItemSpec is its ID; Rc holds its rows too): the
			// selected run's header is filled like a selected row, and the
			// list draws its text on top.
			if id := int32(cd.ItemSpec); b.runSelected(int(id)) {
				widget.FillRect(cd.HDC, win32.ListGroupHeaderRect(b.list, id), pal.Selection)
			}
			return win32.CDRF_DODEFAULT
		}
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		return win32.CDRF_NOTIFYSUBITEMDRAW
	case win32.CDDS_ITEMPREPAINT | win32.CDDS_SUBITEM:
		ref, ok := b.refOf(cd.ItemParam)
		font := t.Fonts.Get(widget.TextBody)
		cd.ClrText = uint32(pal.Text)
		if ok && ref.row < 0 {
			cd.ClrText = uint32(pal.TextSecondary)
		}
		if ok && ref.row >= 0 {
			r := b.view.Groups[ref.group].Rows[ref.row]
			switch cd.SubItem {
			case 1:
				win32.SelectFont(cd.HDC, font)
				if item := int(cd.ItemSpec); win32.ListSelected(b.list) == item {
					// The selection highlight shows the badge's text.
					return win32.CDRF_NEWFONT
				}
				return win32.CDRF_NEWFONT | win32.CDRF_NOTIFYPOSTPAINT
			case 4:
				font = t.Fonts.Get(widget.TextMono)
				cd.ClrText = uint32(pal.TextSecondary)
			case 5:
				cd.ClrText = uint32(toneColor(pal, r.Status.Tone))
			}
		}
		win32.SelectFont(cd.HDC, font)
		return win32.CDRF_NEWFONT
	case win32.CDDS_ITEMPOSTPAINT | win32.CDDS_SUBITEM:
		ref, ok := b.refOf(cd.ItemParam)
		if !ok || ref.row < 0 || cd.SubItem != 1 {
			return win32.CDRF_DODEFAULT
		}
		r := b.view.Groups[ref.group].Rows[ref.row]
		cell := win32.ListSubItemRect(b.list, int(cd.ItemSpec), 1)
		widget.FillRect(cd.HDC, cell, pal.Surface)
		fore, fill := badgeColors(pal, r.Badge.Kind)
		widget.DrawBadge(cd.HDC, t, cell, r.Badge.Text, fore, fill)
	}
	return win32.CDRF_DODEFAULT
}

func (b *restorePage) refOf(param uintptr) (rowRef, bool) {
	p := int(param)
	if p < 1 || p > len(b.rows) {
		return rowRef{}, false
	}
	return b.rows[p-1], true
}

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
