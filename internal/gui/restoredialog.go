package gui

import (
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"os"
	"path/filepath"
	"time"
)

// Control IDs of the Restore window.
const (
	idRestoreStart = 701 + iota
	idRestoreCancel
	idRestoreList
	idRestoreDest
	idRestoreBrowse
	idRestoreIntoBackupDir
	idRestoreDetails
)

// Sizes of the Restore window, in DIPs; it is as wide as the backup plan.
const (
	restoreWidth     = planWidth
	restoreHeight    = 560
	restoreMinWidth  = 560
	restoreMinHeight = 360
	restoreMargin    = planMargin
	restoreHeading   = 24
	browseButton     = 100
	// restoreMaxRows is how many folders the table shows before it scrolls,
	// until the user drags the splitter.
	restoreMaxRows = 5
)

// Columns of the folder table, in DIPs; the first fills (RW-5).
var restoreColumns = []struct {
	title string
	width int32
	right bool
}{
	{view.PlanColumnFolder, 0, false},
	{view.PlanColumnType, 80, false},
	{view.PlanColumnAbout, 80, true},
	{view.ColumnCheck, 130, false},
}

const (
	restoreClass = "RestoreSafeRestore"
	// checkTimerID delays the check until typing stops (RW-6a).
	checkTimerID = 2
	checkDelayMs = 300
)

// restoreDialog is the Restore window (GUI spec 8): the restore's plan, as
// the plan dialog is the backup's. It checks the choices with
// restore.PlanDestination while the user makes them; Start runs the restore
// workflow, which checks again and asks to start, and the window answers
// yes itself, because Start was the confirmation. Then it closes and the
// progress card on Restore backup takes over, as for a verification.
type restoreDialog struct {
	a   *app
	win *dialogWindow

	// The choices.
	when    string
	folders []view.FolderChoice
	checked map[naming.BackupEntry]bool
	dest    string

	// plan is the check of the choices, or after Start the workflow's
	// plan; checkErr is why the check has none; planErr is why the
	// workflow ended before it started.
	plan     *interact.RestorePlan
	checkErr error
	planErr  error
	checking bool
	checkSeq int
	// starting is set from Start until the workflow asks to start or ends.
	starting bool
	view     view.RestoreView
	// listHeight is the table's height in DIPs as the user dragged the
	// splitter, 0 until then; listTop is the table's top in pixels.
	listHeight int32
	listTop    int32
	// userSized is set once the user sizes the window; until then it fits
	// its content, as the backup plan does. fitting is set while it does.
	userSized, fitting bool

	// Controls of the window.
	heading, toLabel win32.HWND
	destEdit, browse win32.HWND
	intoBackupDir    win32.HWND
	list             win32.HWND
	split            *widget.Splitter
	lines            *widget.Panel
	linesSt          *stack
	start, cancel    win32.HWND
	filling          bool
}

// openRestore opens the Restore window on the run runID.
func (a *app) openRestore(runID naming.BackupID) {
	if a.restore != nil || a.snapshot == nil || a.machine.Busy() {
		return
	}
	win, err := newDialogWindow(a.theme, a.hwnd, restoreClass, view.RestoreTitle)
	if err != nil {
		return
	}
	win32.SetStyle(win.hwnd, win32.Style(win.hwnd)|win32.WS_THICKFRAME)
	w := &restoreDialog{a: a, win: win, checked: map[naming.BackupEntry]bool{}, dest: a.lastDestination}
	if w.dest == "" {
		if home, err := os.UserHomeDir(); err == nil {
			w.dest = filepath.Join(home, "Restore")
		}
	}
	w.when = view.RestorePointOf(a.snapshot, runID, time.Now())
	w.folders = view.RestoreFoldersOf(a.snapshot, runID)
	for _, f := range w.folders {
		w.checked[f.Set] = f.Enabled
	}
	win.onCommand = func(id uint16) { w.command(id, win32.BN_CLICKED) }
	win.defID = func() uint16 {
		if w.start != 0 && win32.IsEnabled(w.start) {
			return idRestoreStart
		}
		return 0
	}
	win.onMessage = w.message
	win.onDpi = func(widget.Scale) { w.build() }
	win.panel.OnCommand = w.command
	win.panel.OnNotify = w.notify
	a.restore = w
	yubikey.SetParentWindow(uintptr(win.hwnd))
	s := win.theme.Scale
	win.resize(s.Px(restoreWidth), s.Px(restoreHeight), true)
	win32.Enable(a.hwnd, false)
	w.build()
	w.startCheck(0)
	win32.ShowWindow(win.hwnd, win32.SW_SHOWNORMAL)
	w.focus()
}

// build creates the controls.
func (w *restoreDialog) build() {
	t := w.win.theme
	pal := t.Palette
	p := w.win.panel
	win32.KillTimer(w.win.hwnd, checkTimerID)
	p.Clear()
	w.heading, w.toLabel, w.destEdit, w.browse, w.intoBackupDir, w.list = 0, 0, 0, 0, 0, 0
	w.start, w.cancel = 0, 0
	w.split, w.lines, w.linesSt = nil, nil, nil

	w.view = w.viewOf()
	w.heading = p.Label(w.view.Heading, widget.TextStrong, pal.Text)
	w.toLabel = p.Label(view.RestoreTo, widget.TextSmall, pal.TextSecondary)
	w.destEdit = w.child("EDIT", win32.WS_TABSTOP|win32.WS_BORDER|win32.ES_AUTOHSCROLL, idRestoreDest)
	win32.SetAccessibleName(w.destEdit, view.RestoreTo)
	w.filling = true
	win32.SetText(w.destEdit, w.dest)
	w.filling = false
	w.browse = p.Button(view.RestoreBrowse, idRestoreBrowse)
	w.intoBackupDir = p.Link(view.RestoreIntoBackupDir, idRestoreIntoBackupDir)
	w.buildList()
	if sp, err := widget.NewSplitter(t, p.HWND(), pal.Surface); err == nil {
		p.Adopt(sp.HWND())
		sp.OnMove = w.splitMoved
		w.split = sp
	}
	if lp, err := widget.NewPanel(t, p.HWND(), 0, widget.PanelStyle{Back: pal.Surface}); err == nil {
		p.Adopt(lp.HWND())
		lp.OnCommand = w.command
		w.lines = lp
	}
	w.start = p.PrimaryButton(w.view.Start.Text, idRestoreStart)
	w.cancel = p.Button(w.view.Cancel.Text, idRestoreCancel)
	w.update()
}

// buildList creates the folder table with a checkbox per folder (RW-5).
func (w *restoreDialog) buildList() {
	t := w.win.theme
	lv := w.child(win32.WC_LISTVIEW, win32.WS_TABSTOP|win32.WS_BORDER|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_SHOWSELALWAYS|win32.LVS_NOSORTHEADER, idRestoreList)
	win32.ListSetupPlain(lv, true)
	widget.StyleListHeader(t, lv)
	for i, c := range restoreColumns {
		win32.ListInsertColumn(lv, i, c.title, t.Scale.Px(max(c.width, 60)), c.right)
	}
	win32.SetAccessibleName(lv, view.PlanColumnFolder)
	w.list = lv
	w.filling = true
	for i, f := range w.folders {
		item := win32.ListAddItem(lv, f.Folder, uintptr(i+1))
		win32.ListSetText(lv, item, 1, f.Badge.Text)
		win32.ListSetText(lv, item, 2, f.About)
		win32.ListSetText(lv, item, 3, view.CheckNone)
		win32.ListSetChecked(lv, item, f.Enabled && w.checked[f.Set])
	}
	w.filling = false
}

// child creates a standard control on the page with the body font.
func (w *restoreDialog) child(class string, style uint32, id uintptr) win32.HWND {
	p := w.win.panel
	h, err := win32.CreateWindow(0, class, "", win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, p.HWND(), id)
	if err != nil {
		return 0
	}
	p.Adopt(h)
	win32.SetFont(h, w.win.theme.Fonts.Get(widget.TextBody))
	return h
}

// viewOf words the choices and their check.
func (w *restoreDialog) viewOf() view.RestoreView {
	v := view.RestoreViewOf(w.folders, w.checked, w.when, w.dest, w.plan, w.checkErr, w.checking)
	if w.planErr != nil && (w.plan == nil || !w.plan.HasErrors()) {
		v.Issues = append(v.Issues, view.IssueLine{Text: view.IssueText(w.planErr.Error()), Tone: view.ToneError, Glyph: view.GlyphError})
	}
	return v
}

// update shows the state of the choices: the heading, the checks, the
// lines below the table and Start.
func (w *restoreDialog) update() {
	w.view = w.viewOf()
	win32.SetText(w.heading, w.view.Heading)
	for i, f := range w.folders {
		text := view.CheckNone
		if c, ok := w.view.Checks[f.Set]; ok && f.Enabled && w.checked[f.Set] {
			text = c.Text
		}
		win32.ListSetText(w.list, i, 3, text)
	}
	win32.Invalidate(w.list)
	win32.Enable(w.start, w.view.Start.Enabled && !w.starting)
	w.buildLines()
	w.fitHeight()
	w.layout()
}

// buildLines rebuilds the lines below the table, as the backup plan lays
// out its own (BP-2, RW-6): Space, Unlock, the note, the issues, Show
// details.
func (w *restoreDialog) buildLines() {
	if w.lines == nil {
		return
	}
	t := w.win.theme
	pal := t.Palette
	s := t.Scale
	v := w.view
	w.lines.Clear()
	width := win32.ClientRect(w.win.panel.HWND()).Width() - 2*s.Px(restoreMargin)
	st := newStack(t, w.lines, max(width, s.Px(300)))
	if v.Hint != "" {
		st.para(v.Hint, widget.TextBody, toneColor(pal, v.HintTone), view.GlyphNone)
		st.gap(4)
	} else {
		for _, line := range []view.PlanLine{v.Space, v.Unlock} {
			color := pal.Text
			if line.Tone != view.ToneNeutral && line.Tone != view.ToneSuccess {
				color = toneColor(pal, line.Tone)
			}
			st.labeled(line.Label, line.Text, color, line.Glyph)
			st.gap(4)
		}
	}
	st.gap(8)
	st.para(v.Note, widget.TextSmall, pal.TextSecondary, view.GlyphInfo)
	for _, issue := range v.Issues {
		st.gap(6)
		st.para(issue.Text, widget.TextBody, toneColor(pal, issue.Tone), issue.Glyph)
	}
	if v.Details.Enabled {
		st.gap(8)
		st.row(stackLineHeight, cell{hwnd: w.lines.Link(v.Details.Text, idRestoreDetails), dip: 120})
	}
	w.linesSt = st
}

// layout places the controls.
func (w *restoreDialog) layout() {
	t := w.win.theme
	s := t.Scale
	p := w.win.panel
	area := widget.NewArea(s, win32.ClientRect(p.HWND()))
	area.Inset(restoreMargin, restoreMargin, restoreMargin, restoreMargin)

	// Start and Cancel at the bottom right, as in the backup plan.
	row := widget.NewArea(s, area.Bottom(widget.ButtonHeight))
	win32.SetWindowPos(w.cancel, row.RightPx(buttonWidth(t, w.cancel)))
	row.Right(8)
	win32.SetWindowPos(w.start, row.RightPx(buttonWidth(t, w.start)))
	area.Bottom(12)

	win32.SetWindowPos(w.heading, area.Top(restoreHeading))
	area.Top(10)
	to := widget.NewArea(s, area.Top(widget.ButtonHeight))
	label := to.Left(stackLabelWidth)
	label.Top += (label.Height() - s.Px(stackLineHeight)) / 2
	label.Bottom = label.Top + s.Px(stackLineHeight)
	win32.SetWindowPos(w.toLabel, label)
	win32.SetWindowPos(w.browse, to.Right(browseButton))
	to.Right(8)
	edit := to.Rest()
	edit.Top += (edit.Height() - s.Px(widget.EditHeight)) / 2
	edit.Bottom = edit.Top + s.Px(widget.EditHeight)
	win32.SetWindowPos(w.destEdit, edit)
	area.Top(4)
	link := area.Top(stackLineHeight)
	link.Left += s.Px(stackLabelWidth)
	lw, _ := t.Fonts.Measure(view.RestoreIntoBackupDir, widget.TextSmall)
	link.Right = min(link.Left+lw+s.Px(linkPadding), link.Right)
	win32.SetWindowPos(w.intoBackupDir, link)
	area.Top(12)

	// The table as high as its rows (or as dragged), the splitter, the
	// lines; the table gives way when the lines need the room.
	split := s.Px(widget.SplitterHeight)
	want, least := w.listHeights()
	h := max(min(want, area.Height()-split-w.linesHeight()), least)
	w.listTop = area.R.Top
	list := area.TopPx(h)
	win32.SetWindowPos(w.list, list)
	w.fitColumns(list.Width())
	if w.split != nil {
		win32.SetWindowPos(w.split.HWND(), area.TopPx(split))
	}
	if w.lines != nil {
		win32.SetWindowPos(w.lines.HWND(), area.Rest())
		if w.linesSt != nil {
			w.linesSt.place(0, 0)
		}
	}
}

// listHeights returns the table's height in pixels as its rows (up to
// restoreMaxRows) or the splitter want it, and the least it shows.
func (w *restoreDialog) listHeights() (want, least int32) {
	n := max(len(w.folders), 1)
	least = w.rowsHeight(min(n, tableMinRows))
	want = w.rowsHeight(min(n, restoreMaxRows))
	if w.listHeight > 0 {
		want = w.win.theme.Scale.Px(w.listHeight)
	}
	return max(want, least), least
}

// linesHeight returns the height of the lines below the table in pixels.
func (w *restoreDialog) linesHeight() int32 {
	if w.linesSt == nil {
		return 0
	}
	return w.linesSt.height()
}

// fitHeight sizes the window to its content, as the backup plan does,
// until the user sizes it; at most the height of the screen.
func (w *restoreDialog) fitHeight() {
	if w.userSized || w.list == 0 {
		return
	}
	s := w.win.theme.Scale
	want, _ := w.listHeights()
	// Margins, Start and Cancel, heading, To, the link, the gaps (layout).
	fixed := s.Px(2*restoreMargin+widget.ButtonHeight+12+restoreHeading+10+widget.ButtonHeight+4+stackLineHeight+12) + s.Px(widget.SplitterHeight)
	h := max(fixed+want+w.linesHeight(), s.Px(restoreMinHeight))
	h = min(h, win32.SystemMetric(win32.SM_CYFULLSCREEN, uint32(s))*9/10)
	cur := win32.ClientRect(w.win.hwnd)
	if cur.Height() == h {
		return
	}
	w.fitting = true
	w.win.resize(cur.Width(), h, false)
	w.fitting = false
}

// rowsHeight returns the height in pixels of the table showing n rows.
func (w *restoreDialog) rowsHeight(n int) int32 {
	s := w.win.theme.Scale
	return win32.ListViewHeight(w.list, n) + 2*win32.SystemMetric(win32.SM_CXBORDER, uint32(s)) + s.Px(2)
}

// splitMoved sets the table's height for the splitter's new top.
func (w *restoreDialog) splitMoved(top int32) {
	s := w.win.theme.Scale
	w.listHeight = max(s.Dip(top-w.listTop), 1)
	w.fitHeight() // a window that fits its content grows with the table
	w.layout()
	w.listHeight = s.Dip(win32.WindowRect(w.list).Height()) // as far as it got
}

// fitColumns gives the filling column of the table the width the others
// leave.
func (w *restoreDialog) fitColumns(width int32) {
	s := w.win.theme.Scale
	fixed := int32(0)
	fill := -1
	for i, c := range restoreColumns {
		if c.width == 0 {
			fill = i
			continue
		}
		fixed += s.Px(c.width)
	}
	scroll := win32.SystemMetric(win32.SM_CXVSCROLL, uint32(s)) + s.Px(4)
	if fill >= 0 {
		win32.ListSetColumnWidth(w.list, fill, max(width-fixed-scroll, s.Px(80)))
	}
}

// focus puts the keyboard focus on the table.
func (w *restoreDialog) focus() {
	if w.list != 0 {
		win32.SetFocus(w.list)
	}
}

func (w *restoreDialog) command(id, code uint16) {
	a := w.a
	switch {
	case id == idRestoreDest && code == win32.EN_CHANGE:
		if !w.filling {
			w.dest = win32.Text(w.destEdit)
			w.startCheck(checkDelayMs)
		}
	case code != win32.BN_CLICKED && code != 0:
	case id == idRestoreStart:
		w.startRestore()
	case id == idRestoreCancel || id == win32.IDCANCEL:
		w.cancelPressed()
	case id == idRestoreBrowse:
		if path, ok, err := win32.PickFolder(w.win.hwnd, view.RestoreTitle, w.dest); err == nil && ok {
			win32.SetText(w.destEdit, path) // EN_CHANGE checks it
		}
	case id == idRestoreIntoBackupDir:
		win32.SetText(w.destEdit, view.Path(a.backupDir)) // the configuration may use slashes
	case id == idRestoreDetails:
		if w.plan != nil {
			a.showDetails(w.win.hwnd, view.RestoreDetailsTitle, w.plan.Details)
		}
	}
}

// startRestore starts the restore workflow with the choices (RW-6b); it
// checks again, shows its plan and asks to start, which ask answers.
func (w *restoreDialog) startRestore() {
	a := w.a
	if w.starting || !win32.IsEnabled(w.start) {
		return
	}
	a.lastDestination = w.dest
	w.starting, w.planErr = true, nil
	w.update()
	a.startOperation(opRequest{op: flow.OpRestore, sets: view.Chosen(w.folders, w.checked), destination: w.dest})
}

// cancelPressed handles Cancel, Esc and the close button: it closes
// without writing anything, and ends the workflow when Start began it.
func (w *restoreDialog) cancelPressed() {
	if w.starting {
		w.a.cancelRun()
	}
	w.close()
}

// startCheck checks the choices after delayMs, or now.
func (w *restoreDialog) startCheck(delayMs uint32) {
	w.plan, w.checkErr, w.planErr = nil, nil, nil
	w.checking = true
	w.checkSeq++
	if delayMs > 0 {
		win32.SetTimer(w.win.hwnd, checkTimerID, delayMs)
		w.update()
		return
	}
	w.runCheck()
}

// runCheck checks the choices on a worker goroutine: the folders and the
// free space may be on a slow drive.
func (w *restoreDialog) runCheck() {
	a := w.a
	if v := view.RestoreViewOf(w.folders, w.checked, w.when, w.dest, nil, nil, false); v.Hint != "" && !v.Checking {
		// Nothing to check yet: no folder checked, an empty or relative path.
		w.checking = false
		w.update()
		return
	}
	seq, dest, sets := w.checkSeq, w.dest, view.Chosen(w.folders, w.checked)
	cfg, backupDir, infos := a.opts.Config, a.backupDir, a.snapshot.Sets
	go func() {
		plan, err := restore.PlanDestination(cfg, backupDir, infos, sets, dest)
		a.mu.Lock()
		a.pendingDest = &destCheck{seq: seq, plan: plan, err: err}
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgDestChecked, 0, 0) //nolint:errcheck
	}()
	w.update()
}

// destCheck is the result of a check of the choices.
type destCheck struct {
	seq  int
	plan interact.RestorePlan
	err  error
}

// destChecked shows a check, unless the choices changed since or the
// restore started.
func (w *restoreDialog) destChecked(c *destCheck) {
	if c == nil || c.seq != w.checkSeq || w.starting {
		return
	}
	w.checking = false
	if c.err != nil {
		w.plan, w.checkErr = nil, c.err
	} else {
		plan := c.plan
		w.plan, w.checkErr = &plan, nil
	}
	w.update()
}

// setPlan shows the workflow's plan, made after Start; it replaces the
// check.
func (w *restoreDialog) setPlan(p interact.RestorePlan) {
	w.checkSeq++ // a check still running is older
	w.plan, w.checkErr, w.checking = &p, nil, false
	w.update()
}

// ask answers the workflow's start question: yes after Start, which was
// the confirmation (RW-6b). The window closes, the credential dialogs
// follow and the progress card on Restore backup takes over (RW-9), as for
// a verification (BK-8).
func (w *restoreDialog) ask(answer func(bool, error)) {
	if !w.starting {
		answer(false, nil)
		return
	}
	w.starting = false
	w.close()
	w.a.runStarted()
	answer(true, nil)
}

// workerDone keeps the window open when the restore ended before it
// started (Cancel, a blocked plan): it shows why in place of the check. It
// reports whether it took care of the end.
func (w *restoreDialog) workerDone() bool {
	a := w.a
	r := a.machine.Current()
	if r == nil || !r.Started.IsZero() {
		w.close()
		return false
	}
	a.machine.Dismiss()
	w.starting = false
	w.planErr = r.Err
	w.update()
	return true
}

func (w *restoreDialog) notify(hdr *win32.NMHdr) uintptr {
	if hdr.HwndFrom != w.list || w.list == 0 {
		return 0
	}
	switch hdr.Code {
	case win32.LVN_ITEMCHANGED:
		n := win32.ListChangeOf(hdr)
		if w.filling || n.Changed&win32.LVIF_STATE == 0 {
			return 0
		}
		i := int(win32.ListParam(w.list, int(n.Item))) - 1
		if (n.NewState^n.OldState)&win32.LVIS_STATEIMAGEMASK != 0 && i >= 0 && i < len(w.folders) {
			f := w.folders[i]
			checked := win32.ListChecked(w.list, int(n.Item))
			if checked && !f.Enabled {
				win32.ListSetChecked(w.list, int(n.Item), false)
				return 0
			}
			if w.checked[f.Set] != checked {
				w.checked[f.Set] = checked
				w.startCheck(0)
			}
		}
	case win32.NM_CUSTOMDRAW:
		return w.customDraw(win32.ListDrawOf(hdr))
	}
	return 0
}

// customDraw greys the rows that cannot be restored, colors the Check
// column and draws the type badges.
func (w *restoreDialog) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := w.win.theme
	pal := t.Palette
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		return win32.CDRF_NOTIFYSUBITEMDRAW
	}
	i := int(cd.ItemParam) - 1
	if i < 0 || i >= len(w.folders) {
		return win32.CDRF_DODEFAULT
	}
	f := w.folders[i]
	selected := win32.ListSelected(w.list) == int(cd.ItemSpec)
	switch cd.DrawStage {
	case win32.CDDS_ITEMPREPAINT | win32.CDDS_SUBITEM:
		cd.ClrText = uint32(pal.Text)
		switch {
		case !f.Enabled:
			cd.ClrText = uint32(pal.TextSecondary)
		case cd.SubItem == 1 && !selected:
			return win32.CDRF_NOTIFYPOSTPAINT
		case cd.SubItem == 3 && !selected:
			if c, ok := w.view.Checks[f.Set]; ok && w.checked[f.Set] {
				cd.ClrText = uint32(toneColor(pal, c.Tone))
			} else {
				cd.ClrText = uint32(pal.TextSecondary)
			}
		}
		return win32.CDRF_DODEFAULT
	case win32.CDDS_ITEMPOSTPAINT | win32.CDDS_SUBITEM:
		cell := win32.ListSubItemRect(w.list, int(cd.ItemSpec), 1)
		widget.FillRect(cd.HDC, cell, pal.Surface)
		fore, back := badgeColors(pal, f.Badge.Kind)
		widget.DrawBadge(cd.HDC, t, cell, f.Badge.Text, fore, back)
	}
	return win32.CDRF_DODEFAULT
}

// message handles the window's own messages: the check timer, resizing and
// the minimum size.
func (w *restoreDialog) message(msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch msg {
	case win32.WM_TIMER:
		if wparam == checkTimerID {
			win32.KillTimer(w.win.hwnd, checkTimerID)
			w.runCheck()
			return 0, true
		}
	case win32.WM_SIZING:
		w.userSized = true
		return 0, false
	case win32.WM_SIZE:
		win32.SetWindowPos(w.win.panel.HWND(), win32.ClientRect(w.win.hwnd))
		if !w.fitting {
			w.update() // the lines wrap to the new width
		} else {
			w.layout()
		}
		return 0, true
	case win32.WM_GETMINMAXINFO:
		s := widget.Scale(win32.DpiForWindow(w.win.hwnd))
		r := win32.WindowRectForClient(win32.Rect{Right: s.Px(restoreMinWidth), Bottom: s.Px(restoreMinHeight)}, win32.Style(w.win.hwnd), dialogExStyle, uint32(s))
		win32.MinMaxInfoParam(lparam).MinTrackSize = win32.Point{X: r.Width(), Y: r.Height()}
		return 0, true
	}
	return 0, false
}

// close closes the window.
func (w *restoreDialog) close() {
	a := w.a
	if a.restore != w {
		return
	}
	a.restore = nil
	win32.KillTimer(w.win.hwnd, checkTimerID)
	yubikey.SetParentWindow(uintptr(a.hwnd))
	win32.Enable(a.hwnd, true)
	w.win.destroy()
	a.focusPage()
}
