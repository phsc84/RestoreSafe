package gui

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Control IDs of the Backups page.
const (
	idBackupsFilter = 601 + iota
	idBackupsList
	idBackupsRestore
	idBackupsVerify
	idLogAll
	idLogWarnings
	idLogOpen
	_ // 608 was the log pane's Hide log button; the IDs after it keep their values
	idEmptyBackUp
	idLineButtons = 640 // idLineButtons+i is the button of line i
)

// Context menu items of the list.
const (
	menuRestore = 1 + iota
	menuVerify
	menuCopyName
	menuOpenFolder
)

// Sizes of the Backups page, in DIPs.
const (
	backupsTitleHeight = 32
	filterWidth        = 220
	filterDropHeight   = 300
	actionBarHeight    = 36
	logHeaderHeight    = 30
	minListHeight      = 80
	minLogHeight       = 60
	lineGap            = 4
	logButtonWidth     = 64
)

// Column widths of the list, in DIPs; the status column takes the rest.
var backupColumnWidths = []int32{150, 72, 110, 80, 76}

// backupsPage is the Backups page (spec 7): the runs and their sets, the
// selection's actions, and the log of the selected run.
type backupsPage struct {
	a     *app
	panel *widget.Panel
	acts  actions
	view  view.BackupsPage

	title, filter win32.HWND
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

	splitter                      *widget.Splitter
	logTitle, logAll, logWarnings win32.HWND
	logOpen, logEdit              win32.HWND
	logRatio                      float64
	logPath                       string
	logText                       string
	logSeq                        int
	logFilter                     view.LogFilter

	emptyTitle, emptyLine, emptyButton win32.HWND

	folder    string
	selRun    naming.BackupID
	selSet    string
	collapsed map[naming.BackupID]bool
	logWhen   string
}

type rowRef struct{ group, row int }

func newBackupsPage(a *app) (*backupsPage, error) {
	t := a.theme
	pal := t.Palette
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageBackups, widget.PanelStyle{Back: pal.Surface})
	if err != nil {
		return nil, err
	}
	b := &backupsPage{a: a, panel: panel, acts: actions{}, logRatio: 0.35, collapsed: map[naming.BackupID]bool{}}
	b.title = panel.Label(view.BackupsOf(nil, nil, nil, "", time.Now()).Title, widget.TextTitle, pal.Text)
	b.filter = b.child("COMBOBOX", win32.WS_TABSTOP|win32.WS_VSCROLL|win32.CBS_DROPDOWNLIST, idBackupsFilter)
	win32.SetAccessibleName(b.filter, view.FilterName)
	if b.run, err = newRunCard(a, panel.HWND()); err != nil {
		return nil, err
	}
	if b.lines, err = widget.NewPanel(t, panel.HWND(), 0, widget.PanelStyle{Back: pal.Surface, Outer: pal.Surface, Card: true}); err != nil {
		return nil, err
	}
	b.lines.OnCommand = b.command
	b.list = b.child(win32.WC_LISTVIEW, win32.WS_TABSTOP|win32.WS_BORDER|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_SHOWSELALWAYS|win32.LVS_NOSORTHEADER, idBackupsList)
	win32.ListSetup(b.list)
	widget.StyleListHeader(t, b.list)
	win32.ListEnableInfoTips(b.list)
	for i, c := range view.BackupsOf(nil, nil, nil, "", time.Now()).Columns {
		win32.ListInsertColumn(b.list, i, c, t.Scale.Px(150), i == 3)
	}
	win32.SetAccessibleName(b.list, view.BackupsListName)
	b.barText = panel.Label("", widget.TextBody, pal.Text)
	b.restore = panel.Button(" ", idBackupsRestore)
	b.verify = panel.Button(" ", idBackupsVerify)
	if b.splitter, err = widget.NewSplitter(t, panel.HWND(), pal.Surface); err != nil {
		return nil, err
	}
	panel.Adopt(b.splitter.HWND())
	b.splitter.OnMove = b.splitterMoved
	b.logTitle = panel.Label("", widget.TextStrong, pal.Text)
	lp := view.LogPaneOf()
	b.logAll = b.child("BUTTON", win32.WS_TABSTOP|win32.BS_AUTORADIOBUTTON|win32.BS_PUSHLIKE|win32.WS_GROUP, idLogAll)
	b.logWarnings = b.child("BUTTON", win32.BS_AUTORADIOBUTTON|win32.BS_PUSHLIKE, idLogWarnings)
	b.logOpen = panel.Button(lp.Open, idLogOpen)
	win32.SetText(b.logAll, lp.All)
	win32.SetText(b.logWarnings, lp.Warnings)
	win32.SetChecked(b.logAll, true)
	for _, h := range []win32.HWND{b.logAll, b.logWarnings} {
		widget.StyleButton(t, h, pal.Surface, false)
	}
	b.logEdit = b.child(win32.MSFTEDIT_CLASS, win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL, 0)
	win32.SendMessage(b.logEdit, win32.EM_SETBKGNDCOLOR, 0, uintptr(pal.Surface))
	win32.SendMessage(b.logEdit, win32.EM_EXLIMITTEXT, 0, 64<<20)
	win32.SetAccessibleName(b.logEdit, view.LogPaneName)
	b.emptyTitle = panel.Label("", widget.TextTitle, pal.Text)
	b.emptyLine = panel.Label("", widget.TextBody, pal.TextSecondary)
	b.emptyButton = panel.PrimaryButton(" ", idEmptyBackUp)
	panel.OnCommand = b.command
	panel.OnNotify = b.notify
	b.restyle()
	return b, nil
}

// child creates a control on the page with the body font.
func (b *backupsPage) child(class string, style uint32, id uintptr) win32.HWND {
	h, err := win32.CreateWindow(0, class, "", win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, b.panel.HWND(), id)
	if err != nil {
		return 0
	}
	b.panel.Adopt(h)
	return h
}

// restyle applies the theme's fonts, after creation and DPI changes.
func (b *backupsPage) restyle() {
	t := b.a.theme
	b.panel.Restyle()
	b.run.card.panel.Restyle()
	b.lines.Restyle()
	for _, h := range []win32.HWND{b.filter, b.list, b.logAll, b.logWarnings} {
		win32.SetFont(h, t.Fonts.Get(widget.TextBody))
	}
	win32.SetFont(b.logEdit, t.Fonts.Get(widget.TextMono))
	for i, w := range backupColumnWidths {
		win32.ListSetColumnWidth(b.list, i, t.Scale.Px(w))
	}
	b.linesSig = ""
	b.renderLog()
}

// update shows the current snapshot and operation.
func (b *backupsPage) update() {
	a := b.a
	b.view = view.BackupsOf(a.snapshot, a.opts.Config, a.machine.Current(), b.folder, time.Now())
	v := b.view

	var filters []string
	for _, f := range v.Filters {
		filters = append(filters, f.Text)
	}
	if strings.Join(filters, "\x00") != b.filterSig() {
		win32.ComboSet(b.filter, filters, v.Filter)
	}

	b.showRun()
	empty := v.Empty != nil
	for _, h := range []win32.HWND{b.list, b.barText, b.restore, b.verify, b.logTitle, b.logAll, b.logWarnings, b.logOpen, b.logEdit, b.splitter.HWND()} {
		setShown(h, !empty)
	}
	b.lines.Show(!empty && (v.Retention != nil || len(v.Lines) > 0))
	for _, h := range []win32.HWND{b.emptyTitle, b.emptyLine, b.emptyButton} {
		setShown(h, empty)
	}
	if empty {
		win32.SetText(b.emptyTitle, v.Empty.Title)
		win32.SetText(b.emptyLine, v.Empty.Line)
		win32.SetText(b.emptyButton, v.Empty.Button.Text)
		win32.Enable(b.emptyButton, v.Empty.Button.Enabled)
		b.acts[idEmptyBackUp] = v.Empty.Button.Action
		b.layout()
		return
	}
	b.fillLines()
	b.fillList()
	b.updateBar()
	b.layout()
}

// opRun returns the restore or verification the page shows: running, or
// finished with a result card; nil otherwise. A restore shows once it has
// started (spec RW-9); before, the restore wizard is its plan.
func (b *backupsPage) opRun() *flow.Run {
	r := b.a.machine.Current()
	if r == nil || r.Op == flow.OpBackup || (r.Op == flow.OpRestore && r.Started.IsZero()) {
		return nil
	}
	return r
}

// showRun shows the restore or verification on the run card; it reports
// whether the card appeared or went away.
func (b *backupsPage) showRun() bool {
	return b.run.follow(b.opRun(), b.a.machine.Busy())
}

// updateRun shows a progress report: in full while the page is shown,
// otherwise only the run card.
func (b *backupsPage) updateRun() {
	if b.a.page == view.PageBackups {
		b.update()
		return
	}
	if b.showRun() {
		b.layout()
	}
}

func (b *backupsPage) filterSig() string {
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
func (b *backupsPage) fillLines() {
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
func (b *backupsPage) fillList() {
	v := b.view
	var sig strings.Builder
	for _, g := range v.Groups {
		sig.WriteString(string(g.RunID))
		sig.WriteByte('|')
		sig.WriteString(g.Header)
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
		win32.ListInsertGroup(lv, int32(gi), g.Header, collapsed)
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
func (b *backupsPage) groupsShown() []int {
	out := make([]int, len(b.lastGroups))
	for i := range out {
		out[i] = i
	}
	return out
}

// itemOf returns the list index of the item with param index i+1.
func (b *backupsPage) itemOf(i int) int {
	n := win32.ListItemCount(b.list)
	for item := range n {
		if int(win32.ListParam(b.list, item)) == i+1 {
			return item
		}
	}
	return -1
}

// rowAt returns the group and row of list item i; ok is false for none.
func (b *backupsPage) rowAt(item int) (rowRef, bool) {
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
func (b *backupsPage) updateBar() {
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
	b.highlightRun()
}

// highlightRun shows the sets of the selected run highlighted like a
// selected row: a run is selected through its group header, which the
// list itself can't show as selected.
func (b *backupsPage) highlightRun() {
	for item := range win32.ListItemCount(b.list) {
		ref, ok := b.rowAt(item)
		on := ok && b.selSet == "" && b.selRun != "" && b.view.Groups[ref.group].RunID == b.selRun
		if on != win32.ListHighlighted(b.list, item) {
			win32.ListHighlight(b.list, item, on)
		}
	}
}

// selectItem makes list item i the selection: its set, or the run of a
// placeholder.
func (b *backupsPage) selectItem(item int) {
	ref, ok := b.rowAt(item)
	if !ok {
		return
	}
	g := b.view.Groups[ref.group]
	b.selRun, b.selSet = g.RunID, ""
	if ref.row >= 0 {
		b.selSet = g.Rows[ref.row].Set
	}
	b.selectionChanged(g)
}

// selectRun makes the run of group gi the selection.
func (b *backupsPage) selectRun(gi int) {
	if gi < 0 || gi >= len(b.view.Groups) {
		return
	}
	g := b.view.Groups[gi]
	b.selRun, b.selSet = g.RunID, ""
	win32.ListSelect(b.list, -1)
	b.selectionChanged(g)
}

func (b *backupsPage) selectionChanged(g view.RunGroup) {
	b.updateBar()
	b.showLog(g.LogPath, g.When)
}

// showLogOf shows the Backups page with the run of the log at path
// selected (from "Show log" of the Overview's cards).
func (b *backupsPage) showLogOf(path string) {
	_, runID, ok := naming.ParseLogFileName(filepath.Base(path))
	if !ok {
		return
	}
	b.selRun, b.selSet = runID, ""
	win32.ListSelect(b.list, -1)
	when := ""
	for gi, g := range b.view.Groups {
		if g.RunID == runID {
			when = g.When
			win32.ListSetGroupCollapsed(b.list, int32(gi), false)
			b.collapsed[runID] = false
		}
	}
	b.updateBar()
	b.showLog(path, when)
}

// showLog loads the log at path into the log pane, in the background: the
// backup directory may be on a slow network share.
func (b *backupsPage) showLog(path, when string) {
	b.logWhen = when
	if path == "" {
		b.logPath, b.logText = "", ""
		b.renderLog()
		return
	}
	if path == b.logPath && b.logText != "" {
		return
	}
	b.logPath, b.logText = path, ""
	b.logSeq++
	seq := b.logSeq
	b.renderLog()
	a := b.a
	go func() {
		data, err := os.ReadFile(path)
		text := string(data)
		if err != nil {
			text = ""
		}
		a.mu.Lock()
		a.pendingLog = &loadedLog{seq: seq, path: path, text: text}
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgLogLoaded, 0, 0) //nolint:errcheck
	}()
}

// loadedLog is a log file read in the background.
type loadedLog struct {
	seq  int
	path string
	text string
}

// logLoaded shows the log read in the background, unless another was
// asked for since.
func (b *backupsPage) logLoaded(l *loadedLog) {
	if l == nil || l.seq != b.logSeq || l.path != b.logPath {
		return
	}
	b.logText = l.text
	if b.isLive() {
		// The running operation appends to this log; its output follows.
		b.logText = l.text
	}
	b.renderLog()
}

// isLive reports whether the log pane shows the log the running operation
// writes.
func (b *backupsPage) isLive() bool {
	r := b.a.run
	return r != nil && b.logPath != "" && strings.EqualFold(r.b.LogPath(), b.logPath)
}

// appendLive adds the log lines of the running operation's output when
// the pane shows its log; notices for the screen only (e.g. "Restore
// cancelled.") are not in the log file and stay out.
func (b *backupsPage) appendLive(text string) {
	if !b.isLive() {
		return
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.HasPrefix(line, "[") {
			b.logText += line
		}
	}
	b.renderLog()
}

// reloadLog reads the shown log again: after an operation, the file is
// the exact record.
func (b *backupsPage) reloadLog() {
	if path := b.logPath; path != "" {
		b.logPath = ""
		b.showLog(path, b.logWhen)
	}
}

// renderLog shows the log text with the filter.
func (b *backupsPage) renderLog() {
	win32.SetText(b.logTitle, view.LogPaneTitle(b.logWhen, filepath.Base(b.logPath)))
	if b.logPath == "" {
		win32.SetRichText(b.logEdit, logRTF(nil, b.a.fontPt))
		win32.Enable(b.logOpen, false)
		return
	}
	win32.Enable(b.logOpen, true)
	win32.SetRichText(b.logEdit, logRTF(view.LogLinesOf(b.logText, b.logFilter), b.a.fontPt))
	win32.SendMessage(b.logEdit, win32.WM_VSCROLL, win32.SB_BOTTOM, 0)
}

func (b *backupsPage) command(id, code uint16) {
	a := b.a
	switch {
	case id == idBackupsFilter && code == win32.CBN_SELCHANGE:
		if i := win32.ComboSelected(b.filter); i >= 0 && i < len(b.view.Filters) {
			b.folder = b.view.Filters[i].Folder
			b.update()
		}
	case id == idLogAll && code == win32.BN_CLICKED:
		b.logFilter = view.LogAll
		b.renderLog()
	case id == idLogWarnings && code == win32.BN_CLICKED:
		b.logFilter = view.LogWarnings
		b.renderLog()
	case id == idLogOpen && code == win32.BN_CLICKED:
		if b.logPath != "" {
			a.open(b.logPath, true)
		}
	case code == win32.BN_CLICKED || code == 0:
		if action, ok := b.acts[id]; ok {
			a.do(action)
		}
	}
}

// notify handles the list's notifications.
func (b *backupsPage) notify(hdr *win32.NMHdr) uintptr {
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
func (b *backupsPage) focusChanged() {
	g := int(win32.ListFocusedGroup(b.list))
	if g < 0 || g >= len(b.view.Groups) {
		return
	}
	if b.selSet != "" || b.selRun != b.view.Groups[g].RunID {
		b.selectRun(g)
	}
}

// contextMenu offers the actions of the item under the cursor (spec BK-4).
func (b *backupsPage) contextMenu() {
	screen := win32.CursorPos()
	item := win32.ListHitItem(b.list, win32.ScreenToClient(b.list, screen))
	if item < 0 {
		return
	}
	win32.ListSelect(b.list, item)
	b.selectItem(item)
	ref, _ := b.rowAt(item)
	m := view.BackupsMenu()
	set := ""
	if ref.row >= 0 {
		set = b.view.Groups[ref.group].Rows[ref.row].Set
	}
	busy := b.a.machine.Busy()
	switch win32.ShowMenu(b.a.hwnd, screen, []win32.MenuItem{
		{ID: menuRestore, Text: m.Restore, Disabled: !b.bar.Restore.Enabled || busy},
		{ID: menuVerify, Text: m.Verify, Disabled: !b.bar.Verify.Enabled || busy},
		{},
		{ID: menuCopyName, Text: m.CopyName, Disabled: set == ""},
		{ID: menuOpenFolder, Text: m.OpenFolder},
	}) {
	case menuRestore:
		b.a.do(view.ActionRestore)
	case menuVerify:
		b.a.do(view.ActionVerify)
	case menuCopyName:
		win32.CopyText(b.a.hwnd, set) //nolint:errcheck
	case menuOpenFolder:
		b.a.do(view.ActionOpenBackupDir)
	}
}

// customDraw colors the status, sets the chain in the monospaced font and
// draws the type badges over their cells.
func (b *backupsPage) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := b.a.theme
	pal := t.Palette
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
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
				if item := int(cd.ItemSpec); win32.ListSelected(b.list) == item || win32.ListHighlighted(b.list, item) {
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

func (b *backupsPage) refOf(param uintptr) (rowRef, bool) {
	p := int(param)
	if p < 1 || p > len(b.rows) {
		return rowRef{}, false
	}
	return b.rows[p-1], true
}

// splitterMoved resizes the list and the log pane.
func (b *backupsPage) splitterMoved(top int32) {
	listTop, logBottom := b.splitRange()
	avail := logBottom - listTop
	if avail <= 0 {
		return
	}
	s := b.a.theme.Scale
	logH := logBottom - top - s.Px(widget.SplitterHeight) - s.Px(logHeaderHeight)
	b.logRatio = min(max(float64(logH)/float64(avail), 0.1), 0.8)
	b.layout()
}

// splitRange returns the top of the list and the bottom of the log pane.
func (b *backupsPage) splitRange() (int32, int32) {
	r := win32.WindowRect(b.list)
	top := win32.ScreenToClient(b.panel.HWND(), win32.Point{X: r.Left, Y: r.Top}).Y
	client := win32.ClientRect(b.panel.HWND())
	return top, client.Bottom - b.a.theme.Scale.Px(widget.ContentPaddingY)
}

// layout places the page's controls.
func (b *backupsPage) layout() {
	t := b.a.theme
	s := t.Scale
	area := widget.NewArea(s, win32.ClientRect(b.panel.HWND()))
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)
	top := widget.NewArea(s, area.Top(backupsTitleHeight))
	f := top.Right(filterWidth)
	f.Bottom = f.Top + s.Px(filterDropHeight)
	win32.SetWindowPos(b.filter, f)
	win32.SetWindowPos(b.title, top.Rest())
	area.Top(8)
	if b.run.mode != runHidden {
		b.run.place(area.TopPx(b.run.height(area.Rest().Width())))
		area.Top(widget.CardGap)
	}

	if b.view.Empty != nil {
		win32.SetWindowPos(b.emptyTitle, area.Top(30))
		win32.SetWindowPos(b.emptyLine, area.Top(24))
		area.Top(12)
		row := widget.NewArea(s, area.Top(widget.ButtonHeight))
		r := row.Rest()
		r.Right = r.Left + buttonWidth(t, b.emptyButton)
		win32.SetWindowPos(b.emptyButton, r)
		return
	}

	if win32.IsWindowVisible(b.lines.HWND()) && b.linesStack != nil {
		r := area.TopPx(b.linesHeight)
		win32.SetWindowPos(b.lines.HWND(), r)
		pad := s.Px(widget.CardPadding)
		b.linesStack.place(pad, pad)
		area.Top(widget.CardGap)
	}

	// The log pane at the bottom, the list above it.
	bottom := area.Rest()
	bar := s.Px(actionBarHeight)
	header := s.Px(logHeaderHeight)
	split := s.Px(widget.SplitterHeight)
	avail := bottom.Height() - bar - split - header
	logH := max(int32(float64(avail)*b.logRatio), s.Px(minLogHeight))
	logH = min(logH, max(avail-s.Px(minListHeight), 0))
	listH := max(avail-logH, 0)
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
	y += bar
	win32.SetWindowPos(b.splitter.HWND(), win32.Rect{Left: bottom.Left, Top: y, Right: bottom.Right, Bottom: y + split})
	y += split
	head := widget.NewArea(s, win32.Rect{Left: bottom.Left, Top: y, Right: bottom.Right, Bottom: y + header})
	head.Inset(0, 1, 0, 1)
	for _, h := range []win32.HWND{b.logOpen, b.logWarnings, b.logAll} {
		w, _ := t.Fonts.Measure(win32.Text(h), widget.TextBody)
		win32.SetWindowPos(h, head.RightPx(max(w+s.Px(24), s.Px(logButtonWidth))))
		head.Right(6)
	}
	win32.SetWindowPos(b.logTitle, head.Rest())
	y += header
	win32.SetWindowPos(b.logEdit, win32.Rect{Left: bottom.Left, Top: y, Right: bottom.Right, Bottom: y + logH})
}

// chosen returns the selected sets that can be restored or verified.
func (b *backupsPage) chosen() []naming.BackupEntry {
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
func (b *backupsPage) focus() {
	if b.run.mode != runHidden {
		b.run.focus()
		return
	}
	if b.view.Empty != nil {
		win32.SetFocus(b.emptyButton)
		return
	}
	win32.SetFocus(b.list)
}
