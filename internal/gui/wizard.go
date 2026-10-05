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

// Control IDs of the restore wizard.
const (
	idWizBack = 701 + iota
	idWizNext
	idWizCancel
	idWizList
	idWizDest
	idWizBrowse
	idWizIntoBackupDir
	idWizDetails
)

// Sizes of the restore wizard, in DIPs.
const (
	wizardWidth      = 560
	wizardHeight     = 500
	wizardMinWidth   = 480
	wizardMinHeight  = 440
	wizardMargin     = 18
	wizardTrail      = 24
	wizardHeading    = 24
	wizardNote       = 40
	wizardFooterText = 260
	browseButton     = 100
)

// Columns of the wizard's lists, in DIPs; the last but one fills.
var (
	whenColumns    = []int32{150, 0, 80}
	foldersColumns = []int32{130, 64, 0, 90}
)

const (
	wizardClass = "RestoreSafeWizard"
	// checkTimerID delays the destination check until typing stops (RW-5).
	checkTimerID = 2
	checkDelayMs = 300
)

// restoreWizard is the restore wizard (spec 8): pages 1 to 4 choose and
// check the restore, then it shows the progress and the result. The
// restore workflow runs from page 4 on: it shows its plan there and waits
// for Restore… (ConfirmStart); Back answers no, as nothing is written yet.
type restoreWizard struct {
	a    *app
	win  *dialogWindow
	page int

	// The choices.
	points  []view.RestorePoint
	runID   naming.BackupID
	onSet   string // the set the wizard was opened on, "" for a run
	folders []view.FolderChoice
	checked map[naming.BackupEntry]bool
	dest    string

	// Page 3: the check of the destination.
	destPlan  *interact.RestorePlan
	destErr   error
	checking  bool
	checkSeq  int
	results   *widget.Panel
	resultsSt *stack
	// destTable lists the folders the restore creates (page 3).
	destTable *table

	// Page 4: the workflow's plan and its question.
	plan    *interact.RestorePlan
	planErr error
	answer  func(bool, error)
	checkSt *stack

	// Controls of the current page.
	trail              *widget.Trail
	heading, note      win32.HWND
	noteIcon           win32.HWND
	list               win32.HWND
	destEdit, browse   win32.HWND
	intoBackupDir      win32.HWND
	creates            win32.HWND
	bar                *widget.ProgressBar
	footer             win32.HWND
	back, next, cancel win32.HWND
	filling            bool
	run                *runCard
}

// openWizard opens the restore wizard on the run runID, and on one of its
// sets when set is not "".
func (a *app) openWizard(runID naming.BackupID, set string) {
	if a.wizard != nil || a.snapshot == nil || a.machine.Busy() {
		return
	}
	win, err := newDialogWindow(a.theme, a.hwnd, wizardClass, view.WizardTitle)
	if err != nil {
		return
	}
	win32.SetStyle(win.hwnd, win32.Style(win.hwnd)|win32.WS_THICKFRAME)
	w := &restoreWizard{a: a, win: win, runID: runID, onSet: set, checked: map[naming.BackupEntry]bool{}, dest: a.lastDestination}
	if w.dest == "" {
		if home, err := os.UserHomeDir(); err == nil {
			w.dest = filepath.Join(home, "Restore")
		}
	}
	w.points = view.RestorePointsOf(a.snapshot, time.Now())
	win.onCommand = func(id uint16) { w.command(id, win32.BN_CLICKED) }
	win.defID = func() uint16 {
		if w.next != 0 && win32.IsEnabled(w.next) {
			return idWizNext
		}
		return 0
	}
	win.onMessage = w.message
	win.onDpi = func(widget.Scale) { w.build() }
	win.panel.OnCommand = w.command
	win.panel.OnNotify = w.notify
	a.wizard = w
	yubikey.SetParentWindow(uintptr(win.hwnd))
	s := win.theme.Scale
	win.resize(s.Px(wizardWidth), s.Px(wizardHeight), true)
	win32.Enable(a.hwnd, false)
	w.show(view.WizardWhen)
	win32.ShowWindow(win.hwnd, win32.SW_SHOWNORMAL)
	w.focus()
}

// show switches to page and builds it.
func (w *restoreWizard) show(page int) {
	w.page = page
	w.build()
	w.focus()
}

// build creates the controls of the current page.
func (w *restoreWizard) build() {
	a := w.a
	t := w.win.theme
	pal := t.Palette
	p := w.win.panel
	win32.KillTimer(w.win.hwnd, checkTimerID)
	p.Clear()
	w.trail, w.bar, w.run, w.results, w.destTable = nil, nil, nil, nil, nil
	w.heading, w.note, w.noteIcon, w.list, w.destEdit, w.browse, w.intoBackupDir, w.creates = 0, 0, 0, 0, 0, 0, 0, 0
	w.footer, w.back, w.next, w.cancel = 0, 0, 0, 0
	w.checkSt = nil
	texts := view.WizardTextsOf()

	if w.page <= view.WizardCheck {
		if tr, err := widget.NewTrail(t, p.HWND(), pal.Surface); err == nil {
			p.Adopt(tr.HWND())
			var steps []widget.TrailStep
			for _, s := range view.WizardSteps(w.page) {
				steps = append(steps, widget.TrailStep{Text: s.Text, State: widget.StepState(s.State)})
			}
			tr.Set(steps)
			tr.OnClick = w.goTo
			w.trail = tr
		}
	}
	switch w.page {
	case view.WizardWhen:
		w.heading = p.Label(texts.WhenHeading, widget.TextStrong, pal.Text)
		w.list = w.newList(false, []string{view.ColumnDate, view.ColumnFolders, view.ColumnSize}, whenColumns)
		w.filling = true
		selected := -1
		for i, pt := range w.points {
			item := win32.ListAddItem(w.list, pt.When, uintptr(i+1))
			folders := pt.Folders
			if !pt.Enabled {
				folders = pt.Reason
			}
			win32.ListSetText(w.list, item, 1, folders)
			win32.ListSetText(w.list, item, 2, pt.Size)
			if pt.RunID == w.runID || (selected < 0 && w.runID == "" && pt.Enabled) {
				selected = item
			}
		}
		w.filling = false
		if selected >= 0 {
			win32.ListSelect(w.list, selected)
			w.runID = w.points[selected].RunID
		}
		w.noteIcon, w.note = w.noteRow(texts.WhenNote)
	case view.WizardFolders:
		w.heading = p.Label(texts.FoldersHeading, widget.TextStrong, pal.Text)
		w.list = w.newList(true, []string{view.ColumnFolders, view.PlanColumnType, view.PlanColumnWhy, view.PlanColumnAbout}, foldersColumns)
		w.filling = true
		for i, f := range w.folders {
			item := win32.ListAddItem(w.list, f.Folder, uintptr(i+1))
			win32.ListSetText(w.list, item, 1, f.Badge.Text)
			with := f.With
			if !f.Enabled {
				with = f.Reason
			}
			win32.ListSetText(w.list, item, 2, with)
			win32.ListSetText(w.list, item, 3, f.About)
			win32.ListSetChecked(w.list, item, f.Enabled && w.checked[f.Set])
		}
		w.filling = false
		if len(w.folders) > 0 {
			win32.ListSelect(w.list, 0)
		}
		w.noteIcon, w.note = w.noteRow(texts.FoldersNote)
	case view.WizardDestination:
		w.heading = p.Label(texts.DestHeading, widget.TextStrong, pal.Text)
		w.destEdit = w.child("EDIT", win32.WS_TABSTOP|win32.WS_BORDER|win32.ES_AUTOHSCROLL, idWizDest)
		win32.SetAccessibleName(w.destEdit, texts.DestHeading)
		w.filling = true
		win32.SetText(w.destEdit, w.dest)
		w.filling = false
		w.browse = p.Button(view.WizardBrowse, idWizBrowse)
		w.intoBackupDir = p.Link(view.WizardIntoBackupDir, idWizIntoBackupDir)
		w.creates = p.Label(texts.DestCreates, widget.TextBody, pal.Text)
		if rp, err := widget.NewPanel(t, p.HWND(), 0, widget.PanelStyle{Back: pal.Surface}); err == nil {
			p.Adopt(rp.HWND())
			w.results = rp
			w.destTable = newTable(t, rp.HWND(), 0)
			rp.OnNotify = func(hdr *win32.NMHdr) uintptr {
				r, _ := w.destTable.notify(hdr)
				return r
			}
		}
		w.startCheck(0)
	case view.WizardCheck:
		w.buildCheck()
	case view.WizardProgress, view.WizardResult:
		if rc, err := newRunCardWith(a, t, p.HWND()); err == nil {
			p.Adopt(rc.card.panel.HWND())
			rc.do = w.do
			w.run = rc
		}
	}

	// The footer.
	if w.page <= view.WizardCheck {
		w.footer = p.Label("", widget.TextSmall, pal.TextSecondary)
		w.back = p.Button(view.WizardBack, idWizBack)
		next := view.WizardNext
		if w.page == view.WizardCheck {
			next = view.RestoreButton
		}
		w.next = p.PrimaryButton(next, idWizNext)
		w.cancel = p.Button(view.ButtonCancel, idWizCancel)
	}
	w.update()
}

// newList creates a list on the page with columns of widths (DIPs, 0
// fills).
func (w *restoreWizard) newList(checkboxes bool, columns []string, widths []int32) win32.HWND {
	t := w.win.theme
	lv := w.child(win32.WC_LISTVIEW, win32.WS_TABSTOP|win32.WS_BORDER|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_SHOWSELALWAYS|win32.LVS_NOSORTHEADER, idWizList)
	win32.ListSetupPlain(lv, checkboxes)
	widget.StyleListHeader(t, lv)
	for i, c := range columns {
		win32.ListInsertColumn(lv, i, c, t.Scale.Px(max(widths[i], 60)), i == len(columns)-1)
	}
	win32.SetAccessibleName(lv, columns[0])
	return lv
}

// child creates a standard control on the page with the body font.
func (w *restoreWizard) child(class string, style uint32, id uintptr) win32.HWND {
	p := w.win.panel
	h, err := win32.CreateWindow(0, class, "", win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, p.HWND(), id)
	if err != nil {
		return 0
	}
	p.Adopt(h)
	win32.SetFont(h, w.win.theme.Fonts.Get(widget.TextBody))
	return h
}

// noteRow creates an information note: its icon and its text.
func (w *restoreWizard) noteRow(text string) (win32.HWND, win32.HWND) {
	t := w.win.theme
	p := w.win.panel
	var icon win32.HWND
	if i, err := widget.NewIcon(t, p.HWND(), t.Palette.Surface, widget.TextIconSmall); err == nil {
		p.Adopt(i.HWND())
		i.Set(glyphOf(view.GlyphInfo), t.Palette.AccentText, widget.NoCircle, "")
		icon = i.HWND()
	}
	return icon, p.Paragraph(text, widget.TextSmall, t.Palette.TextSecondary)
}

// buildCheck creates page 4: the workflow's plan, or a marquee until it
// arrives.
func (w *restoreWizard) buildCheck() {
	t := w.win.theme
	pal := t.Palette
	p := w.win.panel
	s := t.Scale
	width := win32.ClientRect(p.HWND()).Width() - 2*s.Px(wizardMargin)
	st := newStack(t, p, max(width, s.Px(300)))
	w.checkSt = st
	switch {
	case w.plan == nil && w.planErr == nil:
		st.para(view.PlanPreparing, widget.TextBody, pal.Text, view.GlyphNone)
		st.gap(8)
		if bar, err := widget.NewProgressBar(p.HWND()); err == nil {
			p.Adopt(bar.HWND())
			bar.Set(-1)
			st.row(10, cell{hwnd: bar.HWND(), fill: true})
		}
	case w.plan == nil:
		st.para(issueOf(w.planErr), widget.TextBody, pal.Error, view.GlyphError)
	default:
		v := view.RestoreCheckOf(*w.plan, w.pointWhen(), time.Now())
		st.para(v.Heading, widget.TextStrong, pal.Text, view.GlyphNone)
		st.gap(10)
		for _, l := range v.Lines {
			color := pal.Text
			if l.Tone == view.ToneError || l.Tone == view.ToneWarning {
				color = toneColor(pal, l.Tone)
			}
			if len(l.Paths) > 0 {
				st.labeledPaths(l.Label, l.Paths, color)
			} else {
				st.labeled(l.Label, l.Text, color, l.Glyph)
			}
			st.gap(4)
		}
		st.gap(8)
		st.para(v.Note, widget.TextSmall, pal.TextSecondary, view.GlyphInfo)
		for _, issue := range v.Issues {
			st.gap(6)
			st.para(issue.Text, widget.TextBody, toneColor(pal, issue.Tone), issue.Glyph)
		}
		if w.planErr != nil && !w.plan.HasErrors() {
			st.gap(6)
			st.para(issueOf(w.planErr), widget.TextBody, pal.Error, view.GlyphError)
		}
		st.gap(8)
		h := p.Link(v.Details.Text, idWizDetails)
		st.row(stackLineHeight, cell{hwnd: h, dip: 120})
	}
}

// issueOf is a workflow error without "Remedy:".
func issueOf(err error) string {
	if err == nil {
		return ""
	}
	return view.IssueText(err.Error())
}

// pointWhen names the chosen restore point.
func (w *restoreWizard) pointWhen() string {
	for _, p := range w.points {
		if p.RunID == w.runID {
			return p.When
		}
	}
	return ""
}

// update shows the state of the current page: the footer and the buttons,
// the destination checks, the progress or the result.
func (w *restoreWizard) update() {
	switch w.page {
	case view.WizardProgress:
		if r := w.a.machine.Current(); r != nil && w.run != nil {
			w.run.showProgress(view.ProgressCardOf(r, time.Now()))
		}
	case view.WizardResult:
		if r := w.a.machine.Current(); r != nil && w.run != nil && w.run.mode != runResult {
			if c := view.ResultCardOf(r); c != nil {
				c.Done.Text = view.WizardClose
				w.run.showResult(*c)
			}
		}
	}
	if w.page <= view.WizardCheck {
		next := false
		switch w.page {
		case view.WizardWhen:
			for _, p := range w.points {
				next = next || (p.RunID == w.runID && p.Enabled)
			}
		case view.WizardFolders:
			next = view.SelectionFooter(w.folders, w.checked) != ""
		case view.WizardDestination:
			v := view.DestinationOf(w.dest, w.destPlan, w.destErr, w.checking)
			next = v.Next
			w.showResults(v)
		case view.WizardCheck:
			next = w.answer != nil && w.plan != nil && !w.plan.HasErrors()
		}
		footer := ""
		if w.page >= view.WizardFolders {
			footer = view.SelectionFooter(w.folders, w.checked)
		}
		win32.SetText(w.footer, footer)
		win32.Enable(w.back, w.page > view.WizardWhen)
		win32.Enable(w.next, next)
	}
	w.layout()
}

// showResults rebuilds the destination checks.
func (w *restoreWizard) showResults(v view.DestinationView) {
	if w.results == nil {
		return
	}
	t := w.win.theme
	pal := t.Palette
	s := t.Scale
	w.results.Clear()
	width := win32.ClientRect(w.win.panel.HWND()).Width() - 2*s.Px(wizardMargin)
	st := newStack(t, w.results, max(width, s.Px(300)))
	if v.Hint != "" {
		st.para(v.Hint, widget.TextBody, pal.TextSecondary, view.GlyphNone)
	}
	w.destTable.show(v.Hint == "")
	if v.Hint == "" {
		w.destTable.set(v.Folders)
		st.table(w.destTable)
	}
	if v.Remedy != "" {
		st.gap(6)
		st.para(v.Remedy, widget.TextSmall, pal.TextSecondary, view.GlyphNone)
	}
	if v.Space != nil {
		st.gap(10)
		st.para(v.Space.Text, widget.TextBody, toneColor(pal, v.Space.Tone), v.Space.Glyph)
	}
	w.resultsSt = st
}

// layout places the controls of the current page.
func (w *restoreWizard) layout() {
	t := w.win.theme
	s := t.Scale
	p := w.win.panel
	area := widget.NewArea(s, win32.ClientRect(p.HWND()))
	area.Inset(wizardMargin, wizardMargin, wizardMargin, wizardMargin)
	if w.trail != nil {
		win32.SetWindowPos(w.trail.HWND(), area.Top(wizardTrail))
		area.Top(12)
	}
	if w.back != 0 {
		row := widget.NewArea(s, area.Bottom(widget.ButtonHeight))
		for _, b := range []win32.HWND{w.cancel, w.next, w.back} {
			win32.SetWindowPos(b, row.RightPx(max(buttonWidth(t, b), s.Px(96))))
			row.Right(8)
		}
		win32.SetWindowPos(w.footer, row.Rest())
		area.Bottom(12)
	}
	switch w.page {
	case view.WizardWhen, view.WizardFolders:
		win32.SetWindowPos(w.heading, area.Top(wizardHeading))
		area.Top(6)
		note := area.Bottom(wizardNote)
		if w.noteIcon != 0 {
			icon := note
			icon.Right = icon.Left + s.Px(iconWidth)
			icon.Bottom = icon.Top + s.Px(stackLineHeight)
			win32.SetWindowPos(w.noteIcon, icon)
			note.Left += s.Px(iconWidth)
		}
		win32.SetWindowPos(w.note, note)
		area.Bottom(8)
		list := area.Rest()
		win32.SetWindowPos(w.list, list)
		w.fitColumns(list.Width())
	case view.WizardDestination:
		win32.SetWindowPos(w.heading, area.Top(wizardHeading))
		area.Top(6)
		row := widget.NewArea(s, area.Top(widget.ButtonHeight))
		win32.SetWindowPos(w.browse, row.Right(browseButton))
		row.Right(8)
		edit := row.Rest()
		edit.Top += (edit.Height() - s.Px(widget.EditHeight)) / 2
		edit.Bottom = edit.Top + s.Px(widget.EditHeight)
		win32.SetWindowPos(w.destEdit, edit)
		area.Top(4)
		link := area.Top(stackLineHeight)
		lw, _ := t.Fonts.Measure(view.WizardIntoBackupDir, widget.TextSmall)
		link.Right = min(link.Left+lw+s.Px(linkPadding), link.Right)
		win32.SetWindowPos(w.intoBackupDir, link)
		area.Top(12)
		win32.SetWindowPos(w.creates, area.Top(stackLineHeight))
		area.Top(6)
		if w.results != nil {
			win32.SetWindowPos(w.results.HWND(), area.Rest())
			if w.resultsSt != nil {
				w.resultsSt.place(0, 0)
			}
		}
	case view.WizardCheck:
		if w.checkSt != nil {
			r := area.Rest()
			w.checkSt.place(r.Left, r.Top)
		}
	case view.WizardProgress, view.WizardResult:
		if w.run != nil && w.run.mode != runHidden {
			w.run.place(area.TopPx(w.run.height(area.Rest().Width())))
		}
	}
}

// fitColumns gives the filling column of the page's list the width the
// others leave.
func (w *restoreWizard) fitColumns(width int32) {
	s := w.win.theme.Scale
	widths := whenColumns
	if w.page == view.WizardFolders {
		widths = foldersColumns
	}
	fixed := int32(0)
	fill := -1
	for i, c := range widths {
		if c == 0 {
			fill = i
			continue
		}
		fixed += s.Px(c)
	}
	scroll := win32.SystemMetric(win32.SM_CXVSCROLL, uint32(s)) + s.Px(4)
	if fill >= 0 {
		win32.ListSetColumnWidth(w.list, fill, max(width-fixed-scroll, s.Px(80)))
	}
}

// focus puts the keyboard focus on the page's main control.
func (w *restoreWizard) focus() {
	switch {
	case w.list != 0:
		win32.SetFocus(w.list)
	case w.destEdit != 0:
		win32.SetFocus(w.destEdit)
	case w.run != nil:
		w.run.focus()
	case w.next != 0 && win32.IsEnabled(w.next):
		win32.SetFocus(w.next)
	case w.cancel != 0:
		win32.SetFocus(w.cancel)
	}
}

func (w *restoreWizard) command(id, code uint16) {
	a := w.a
	switch {
	case id == idWizDest && code == win32.EN_CHANGE:
		if !w.filling {
			w.dest = win32.Text(w.destEdit)
			w.startCheck(checkDelayMs)
		}
	case code != win32.BN_CLICKED && code != 0:
	case id == idWizNext:
		w.forward()
	case id == idWizBack:
		w.backward()
	case id == idWizCancel || id == win32.IDCANCEL:
		w.cancelPressed()
	case id == idWizBrowse:
		if path, ok, err := win32.PickFolder(w.win.hwnd, view.WizardTitle, w.dest); err == nil && ok {
			win32.SetText(w.destEdit, path) // EN_CHANGE checks it
		}
	case id == idWizIntoBackupDir:
		win32.SetText(w.destEdit, a.backupDir)
	case id == idWizDetails:
		if w.plan != nil {
			a.showDetails(w.win.hwnd, view.RestoreDetailsTitle, w.plan.Details)
		}
	}
}

// forward goes to the next page; on page 4 it starts the restore.
func (w *restoreWizard) forward() {
	a := w.a
	if !win32.IsEnabled(w.next) {
		return
	}
	switch w.page {
	case view.WizardWhen:
		w.folders = view.RestoreFoldersOf(a.snapshot, w.runID, time.Now())
		w.checked = map[naming.BackupEntry]bool{}
		for _, f := range w.folders {
			w.checked[f.Set] = f.Enabled && (w.onSet == "" || f.Set.String() == w.onSet)
		}
		if view.SelectionFooter(w.folders, w.checked) == "" {
			for _, f := range w.folders {
				w.checked[f.Set] = f.Enabled
			}
		}
		w.show(view.WizardFolders)
	case view.WizardFolders:
		w.show(view.WizardDestination)
	case view.WizardDestination:
		a.lastDestination = w.dest
		w.toCheck()
	case view.WizardCheck:
		answer := w.answer
		w.answer = nil
		a.machine.Confirmed(time.Now())
		win32.Enable(a.hwnd, true) // the main window stays usable for reading (RW-9)
		w.show(view.WizardProgress)
		a.refreshRun()
		answer(true, nil)
	}
}

// toCheck opens page 4 and starts the restore workflow, which plans and
// asks.
func (w *restoreWizard) toCheck() {
	w.plan, w.planErr, w.answer = nil, nil, nil
	w.show(view.WizardCheck)
	w.a.startOperation(opRequest{op: flow.OpRestore, sets: w.chosen(), destination: w.dest})
}

// chosen returns the checked sets that can be restored.
func (w *restoreWizard) chosen() []naming.BackupEntry {
	var sets []naming.BackupEntry
	for _, f := range w.folders {
		if f.Enabled && w.checked[f.Set] {
			sets = append(sets, f.Set)
		}
	}
	return sets
}

// backward goes to the previous page; leaving page 4 ends the workflow,
// which has written nothing yet.
func (w *restoreWizard) backward() {
	if w.page == view.WizardCheck {
		w.a.cancelRun()
	}
	if w.page > view.WizardWhen && w.page <= view.WizardCheck {
		w.show(w.page - 1)
	}
}

// cancelPressed handles Cancel, Esc and the close button: it closes on
// pages 1 to 4 and after the result, and asks before cancelling a running
// restore.
func (w *restoreWizard) cancelPressed() {
	switch w.page {
	case view.WizardProgress:
		w.a.confirmCancel()
	case view.WizardResult:
		w.a.dismiss()
	default:
		if w.page == view.WizardCheck {
			w.a.cancelRun()
		}
		w.close()
	}
}

// do runs the actions of the progress and result pages.
func (w *restoreWizard) do(action view.Action) {
	switch action {
	case view.ActionShowLog:
		w.showLog()
	default:
		w.a.do(action)
	}
}

// showLog shows the log of the restore in a viewer over the wizard (RW-8).
func (w *restoreWizard) showLog() {
	path := ""
	if w.a.run != nil {
		path = w.a.run.b.LogPath()
	} else if r := w.a.machine.Current(); r != nil {
		path = r.LogPath
	}
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	w.a.showLog(w.win.hwnd, view.LogPaneTitle("", filepath.Base(path)), string(data))
}

// startCheck checks the destination after delayMs, or now.
func (w *restoreWizard) startCheck(delayMs uint32) {
	w.destPlan, w.destErr = nil, nil
	w.checking = true
	w.checkSeq++
	if delayMs > 0 {
		win32.SetTimer(w.win.hwnd, checkTimerID, delayMs)
		w.update()
		return
	}
	w.runCheck()
}

// runCheck checks the destination on a worker goroutine: the folders and
// the free space may be on a slow drive.
func (w *restoreWizard) runCheck() {
	a := w.a
	v := view.DestinationOf(w.dest, nil, nil, false)
	if v.Hint != "" && !v.Checking {
		// Nothing to check yet: an empty or relative path.
		w.checking = false
		w.update()
		return
	}
	seq, dest, sets := w.checkSeq, w.dest, w.chosen()
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

// destCheck is the result of a destination check.
type destCheck struct {
	seq  int
	plan interact.RestorePlan
	err  error
}

// destChecked shows a destination check, unless the path changed since.
func (w *restoreWizard) destChecked(c *destCheck) {
	if c == nil || c.seq != w.checkSeq || w.page != view.WizardDestination {
		return
	}
	w.checking = false
	if c.err != nil {
		w.destPlan, w.destErr = nil, c.err
	} else {
		plan := c.plan
		w.destPlan, w.destErr = &plan, nil
	}
	w.update()
}

// setPlan shows the workflow's plan on page 4.
func (w *restoreWizard) setPlan(p interact.RestorePlan) {
	w.plan = &p
	if w.page == view.WizardCheck {
		w.build()
	}
}

// ask waits for Restore…; the answer goes to the workflow.
func (w *restoreWizard) ask(answer func(bool, error)) {
	if w.page != view.WizardCheck {
		answer(false, nil)
		return
	}
	w.answer = answer
	w.update()
	w.focus()
}

// workerDone shows how the restore ended. It reports whether the wizard
// took care of the end: an end before the start (Back, Cancel, a blocked
// plan, a cancelled password) leaves no result.
func (w *restoreWizard) workerDone() bool {
	a := w.a
	r := a.machine.Current()
	if r == nil {
		return false
	}
	switch {
	case r.Started.IsZero():
		// Back, Cancel, or the plan blocked the start: page 4 says why.
		a.machine.Dismiss()
		w.answer = nil
		if w.page == view.WizardCheck {
			w.planErr = r.Err
			w.build()
		}
		return true
	case view.ResultCardOf(r) == nil:
		// A credential dialog was cancelled: nothing was written. Back to
		// the check, which plans again.
		a.machine.Dismiss()
		win32.Enable(a.hwnd, false)
		w.toCheck()
		return true
	}
	w.show(view.WizardResult)
	return false
}

func (w *restoreWizard) notify(hdr *win32.NMHdr) uintptr {
	if hdr.HwndFrom != w.list {
		return 0
	}
	switch hdr.Code {
	case win32.LVN_ITEMCHANGED:
		n := win32.ListChangeOf(hdr)
		if w.filling || n.Changed&win32.LVIF_STATE == 0 {
			return 0
		}
		i := int(win32.ListParam(w.list, int(n.Item))) - 1
		switch w.page {
		case view.WizardWhen:
			if n.NewState&win32.LVIS_SELECTED != 0 && i >= 0 && i < len(w.points) {
				w.runID = w.points[i].RunID
				w.update()
			}
		case view.WizardFolders:
			if (n.NewState^n.OldState)&win32.LVIS_STATEIMAGEMASK != 0 && i >= 0 && i < len(w.folders) {
				f := w.folders[i]
				checked := win32.ListChecked(w.list, int(n.Item))
				if checked && !f.Enabled {
					win32.ListSetChecked(w.list, int(n.Item), false)
					return 0
				}
				w.checked[f.Set] = checked
				w.update()
			}
		}
	case win32.NM_DBLCLK:
		if w.page == view.WizardWhen {
			w.forward()
		}
	case win32.NM_CUSTOMDRAW:
		return w.customDraw(win32.ListDrawOf(hdr))
	}
	return 0
}

// customDraw greys the rows that cannot be restored and draws the type
// badges.
func (w *restoreWizard) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := w.win.theme
	pal := t.Palette
	i := int(cd.ItemParam) - 1
	enabled := true
	switch {
	case w.page == view.WizardWhen && i >= 0 && i < len(w.points):
		enabled = w.points[i].Enabled
	case w.page == view.WizardFolders && i >= 0 && i < len(w.folders):
		enabled = w.folders[i].Enabled
	}
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		return win32.CDRF_NOTIFYSUBITEMDRAW
	case win32.CDDS_ITEMPREPAINT | win32.CDDS_SUBITEM:
		cd.ClrText = uint32(pal.Text)
		if !enabled {
			cd.ClrText = uint32(pal.TextSecondary)
		}
		if w.page == view.WizardFolders && cd.SubItem == 1 && enabled && win32.ListSelected(w.list) != int(cd.ItemSpec) {
			return win32.CDRF_NOTIFYPOSTPAINT
		}
		return win32.CDRF_DODEFAULT
	case win32.CDDS_ITEMPOSTPAINT | win32.CDDS_SUBITEM:
		if w.page != view.WizardFolders || i < 0 || i >= len(w.folders) {
			return win32.CDRF_DODEFAULT
		}
		cell := win32.ListSubItemRect(w.list, int(cd.ItemSpec), 1)
		widget.FillRect(cd.HDC, cell, pal.Surface)
		b := w.folders[i].Badge
		fore, back := badgeColors(pal, b.Kind)
		widget.DrawBadge(cd.HDC, t, cell, b.Text, fore, back)
	}
	return win32.CDRF_DODEFAULT
}

// message handles the window's own messages: the check timer, resizing and
// the minimum size.
func (w *restoreWizard) message(msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch msg {
	case win32.WM_TIMER:
		if wparam == checkTimerID {
			win32.KillTimer(w.win.hwnd, checkTimerID)
			w.runCheck()
			return 0, true
		}
	case win32.WM_SIZE:
		win32.SetWindowPos(w.win.panel.HWND(), win32.ClientRect(w.win.hwnd))
		switch w.page {
		case view.WizardCheck:
			w.build()
		case view.WizardDestination:
			w.update()
		default:
			w.layout()
		}
		return 0, true
	case win32.WM_GETMINMAXINFO:
		s := widget.Scale(win32.DpiForWindow(w.win.hwnd))
		r := win32.WindowRectForClient(win32.Rect{Right: s.Px(wizardMinWidth), Bottom: s.Px(wizardMinHeight)}, win32.Style(w.win.hwnd), dialogExStyle, uint32(s))
		win32.MinMaxInfoParam(lparam).MinTrackSize = win32.Point{X: r.Width(), Y: r.Height()}
		return 0, true
	}
	return 0, false
}

// close closes the wizard.
func (w *restoreWizard) close() {
	a := w.a
	if a.wizard != w {
		return
	}
	a.wizard = nil
	win32.KillTimer(w.win.hwnd, checkTimerID)
	yubikey.SetParentWindow(uintptr(a.hwnd))
	win32.Enable(a.hwnd, true)
	w.win.destroy()
	a.focusPage()
}

// goTo goes back to a completed step (RW-1); leaving page 4 ends the
// workflow, which has written nothing yet.
func (w *restoreWizard) goTo(page int) {
	if page >= w.page || w.page > view.WizardCheck {
		return
	}
	if w.page == view.WizardCheck {
		w.a.cancelRun()
	}
	w.show(page)
}
