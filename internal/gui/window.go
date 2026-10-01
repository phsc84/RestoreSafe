// Package gui is the graphical frontend of RestoreSafe: a native Win32 main
// window with screens and dialogs (see docs/SPEC-restoresafe-gui.md).
package gui

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

// Options configure the GUI.
type Options struct {
	Version    string
	ExeDir     string
	ConfigPath string
	Config     *config.Config
}

// Control IDs and application messages.
const (
	idConfigOpen = 101 + iota
	idBackupOpen
	idBackup
	idRestore
	idVerify
	idRecheck
	idOpButton // idOpButton+i is button i of the operation screen

	idTree       = 201
	idDestEdit   = 202
	idDestBrowse = 203
	idDestCheck  = 204

	msgHealthDone = win32.WM_APP + 1
	msgBridge     = win32.WM_APP + 2 // wparam: flow.NoteQuestion, flow.NoteOutput, flow.NoteProgress
	msgWorkerDone = win32.WM_APP + 3
)

// Pages of the main window.
const (
	pageHome = iota
	pageOperation
)

const (
	windowClass   = "RestoreSafeMainWindow"
	windowStyle   = win32.WS_OVERLAPPEDWINDOW | win32.WS_CLIPCHILDREN
	windowExStyle = win32.WS_EX_CONTROLPARENT
	// goversioninfo numbers resources in order: the manifest gets ID 1 and
	// the icon group ID 2; without a manifest the icon group is ID 1.
	appIconID         = 2
	appIconIDFallback = 1
)

// app is the main window. There is one per process; the window procedure
// reaches it through theApp.
type app struct {
	opts      Options
	backupDir string

	hwnd     win32.HWND
	dpi      uint32
	font     windows.Handle
	boldFont windows.Handle // headings
	monoFont windows.Handle // log pane
	fontFace string
	fontPt   int
	page     int
	modal    win32.HWND // open input dialog, if any

	op struct {
		title, detail, progress win32.HWND
		report, log, tree       win32.HWND
		destLabel, destEdit     win32.HWND
		destBrowse, destCheck   win32.HWND
		destNote                win32.HWND
		buttons                 [opButtons]win32.HWND
	}
	opButtons      []opButton
	opTitleStatus  interact.Status
	opContent      opContent
	opShowProgress bool
	// Selection and destination screens.
	treeNodes    map[win32.TreeItem]selectionNode
	selectRuns   []catalog.BackupRunSummary
	selectAction string // "restore" or "verify"
	destDefault  string // the backup directory, for "restore into the backup directory"
	progressText string
	run          *runState
	opReport     *interact.Report // preflight report on screen, re-rendered on DPI changes

	home struct {
		configLabel, configPath, configOpen win32.HWND
		backupLabel, backupPath, backupOpen win32.HWND
		report, status                      win32.HWND
		backup, restore, verify, recheck    win32.HWND
	}
	state homeState

	mu            sync.Mutex
	pendingHealth *health.Result
}

var theApp *app

// Run shows the main window and runs the message loop until it is closed. It
// must be called from the main goroutine before any other window is created.
func Run(opts Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := win32.InitCommonControls(); err != nil {
		return err
	}
	if err := win32.InitCOM(); err != nil {
		return err
	}
	if err := win32.LoadRichEdit(); err != nil {
		return err
	}
	a := &app{opts: opts, backupDir: fsx.ResolveDir(opts.Config.BackupDirectory, opts.ExeDir)}
	theApp = a
	if err := a.createWindow(); err != nil {
		return err
	}
	a.startHealthCheck()

	var msg win32.Msg
	for {
		ok, err := win32.GetMessage(&msg)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if !win32.IsDialogMessage(a.hwnd, &msg) {
			win32.TranslateAndDispatch(&msg)
		}
	}
}

// ShowError shows a message box for an error that prevents the GUI from
// starting (e.g. an unreadable configuration).
func ShowError(title, message string) {
	win32.MessageBox(0, message, "RestoreSafe - "+title, win32.MB_OK|win32.MB_ICONERROR)
}

func (a *app) createWindow() error {
	work, dpi := win32.CursorMonitor()
	s := widget.Scale(dpi)
	wc := win32.WndClassEx{
		WndProc:    windows.NewCallback(wndProc),
		Instance:   win32.ModuleHandle(),
		Icon:       appIcon(win32.SystemMetric(win32.SM_CXICON, dpi)),
		IconSm:     appIcon(win32.SystemMetric(win32.SM_CXSMICON, dpi)),
		Cursor:     win32.ArrowCursor(),
		Background: win32.SysColorBrush(win32.COLOR_WINDOW),
		ClassName:  win32.UTF16(windowClass),
	}
	if err := win32.RegisterClass(&wc); err != nil {
		return err
	}

	// Default size, centered in the work area of the monitor with the cursor.
	frame := win32.WindowRectForClient(win32.Rect{Right: s.Px(windowWidth), Bottom: s.Px(windowHeight)}, windowStyle, windowExStyle, dpi)
	w := min(frame.Width(), work.Width())
	h := min(frame.Height(), work.Height())
	x := work.Left + (work.Width()-w)/2
	y := work.Top + (work.Height()-h)/2

	hwnd, err := win32.CreateWindow(windowExStyle, windowClass, "RestoreSafe "+a.opts.Version, windowStyle, x, y, w, h, 0, 0)
	if err != nil {
		return err
	}
	a.hwnd = hwnd
	a.dpi = win32.DpiForWindow(hwnd)
	yubikey.SetParentWindow(uintptr(hwnd))

	if err := a.createFonts(); err != nil {
		return err
	}
	if err := a.createHome(); err != nil {
		return err
	}
	if err := a.createOperation(); err != nil {
		return err
	}
	a.applyFonts()
	a.setAccessibleNames()
	a.showPage(pageHome)
	a.layout()
	win32.ShowWindow(hwnd, win32.SW_SHOWNORMAL)
	return nil
}

// createFonts creates the message font, a bold variant for headings, and a
// monospaced font for the log, for the current DPI, and applies them; the
// previous fonts are deleted afterwards.
func (a *app) createFonts() error {
	lf, err := win32.MessageFont(a.dpi)
	if err != nil {
		return err
	}
	font, err := win32.CreateFont(&lf)
	if err != nil {
		return err
	}
	bold := lf
	bold.Weight = win32.FW_BOLD
	bold.Height = lf.Height * 6 / 5
	boldFont, err := win32.CreateFont(&bold)
	if err != nil {
		win32.DeleteObject(font)
		return err
	}
	mono := lf
	copy(mono.FaceName[:], windows.StringToUTF16("Consolas"))
	monoFont, err := win32.CreateFont(&mono)
	if err != nil {
		win32.DeleteObject(font)
		win32.DeleteObject(boldFont)
		return err
	}
	old := []windows.Handle{a.font, a.boldFont, a.monoFont}
	a.font, a.boldFont, a.monoFont = font, boldFont, monoFont
	a.fontFace = lf.Face()
	a.fontPt = max(int((-lf.Height*72+int32(a.dpi)/2)/int32(a.dpi)), 8)
	a.applyFonts()
	for _, h := range old {
		win32.DeleteObject(h)
	}
	return nil
}

// applyFonts sets the fonts of all controls and the rich edits' zoom and
// margins for the current DPI.
func (a *app) applyFonts() {
	for _, c := range a.controls() {
		win32.SetFont(c, a.font)
	}
	if a.op.title != 0 {
		win32.SetFont(a.op.title, a.boldFont)
	}
	if a.op.log != 0 {
		win32.SetFont(a.op.log, a.monoFont)
	}
	pad := uintptr(widget.Scale(a.dpi).Px(reportPadding))
	for _, re := range []win32.HWND{a.home.report, a.op.report, a.op.log} {
		if re == 0 {
			continue
		}
		// The rich edit lays out RTF point sizes at the system DPI.
		win32.SendMessage(re, win32.EM_SETZOOM, uintptr(a.dpi), uintptr(win32.DpiForSystem()))
		win32.SendMessage(re, win32.EM_SETMARGINS, win32.EC_LEFTMARGIN|win32.EC_RIGHTMARGIN, pad|pad<<16)
	}
}

// controls returns the controls that use the message font.
func (a *app) controls() []win32.HWND {
	h := &a.home
	all := []win32.HWND{h.configLabel, h.configPath, h.configOpen, h.backupLabel, h.backupPath, h.backupOpen, h.report, h.status, h.backup, h.restore, h.verify, h.recheck,
		a.op.detail, a.op.report, a.op.tree, a.op.destLabel, a.op.destEdit, a.op.destBrowse, a.op.destCheck, a.op.destNote}
	all = append(all, a.op.buttons[:]...)
	out := all[:0]
	for _, c := range all {
		if c != 0 {
			out = append(out, c)
		}
	}
	return out
}

func (a *app) homeControls() []win32.HWND {
	h := &a.home
	return []win32.HWND{h.configLabel, h.configPath, h.configOpen, h.backupLabel, h.backupPath, h.backupOpen, h.report, h.status, h.backup, h.restore, h.verify, h.recheck}
}

// createOperation creates the operation screen's controls, hidden.
func (a *app) createOperation() error {
	o := &a.op
	var err error
	create := func(class string, style uint32, id uintptr) win32.HWND {
		if err != nil {
			return 0
		}
		var c win32.HWND
		c, err = win32.CreateWindow(0, class, "", win32.WS_CHILD|style, 0, 0, 0, 0, a.hwnd, id)
		return c
	}
	o.title = create("STATIC", win32.SS_NOPREFIX|win32.SS_CENTERIMAGE, 0)
	o.detail = create("STATIC", win32.SS_NOPREFIX|win32.SS_CENTERIMAGE|win32.SS_PATHELLIPSIS, 0)
	o.progress = create(win32.PROGRESS_CLASS, 0, 0)
	o.report = create(win32.MSFTEDIT_CLASS, win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL, 0)
	o.log = create(win32.MSFTEDIT_CLASS, win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL, 0)
	o.tree = create(win32.WC_TREEVIEW, win32.WS_TABSTOP|win32.WS_BORDER|win32.TVS_HASBUTTONS|win32.TVS_HASLINES|win32.TVS_LINESATROOT|win32.TVS_SHOWSELALWAYS, idTree)
	o.destLabel = create("STATIC", 0, 0) // &-prefix: Alt+F moves to the field after it
	o.destEdit = create("EDIT", win32.WS_TABSTOP|win32.WS_BORDER|win32.ES_AUTOHSCROLL, idDestEdit)
	o.destBrowse = create("BUTTON", win32.WS_TABSTOP|win32.BS_PUSHBUTTON, idDestBrowse)
	o.destCheck = create("BUTTON", win32.WS_TABSTOP|win32.BS_AUTOCHECKBOX, idDestCheck)
	o.destNote = create("STATIC", win32.SS_NOPREFIX, 0)
	for i := range o.buttons {
		o.buttons[i] = create("BUTTON", win32.WS_TABSTOP|win32.BS_PUSHBUTTON, uintptr(idOpButton+i))
	}
	if err != nil {
		return err
	}
	bg := uintptr(win32.SysColor(win32.COLOR_WINDOW))
	win32.SendMessage(o.report, win32.EM_SETBKGNDCOLOR, 0, bg)
	win32.SendMessage(o.log, win32.EM_SETBKGNDCOLOR, 0, bg)
	win32.SendMessage(o.log, win32.EM_SETTEXTMODE, win32.TM_PLAINTEXT, 0)
	win32.SendMessage(o.log, win32.EM_EXLIMITTEXT, 0, 64<<20)
	win32.SendMessage(o.progress, win32.PBM_SETRANGE32, 0, 1000)
	return nil
}

// showPage shows the controls of page and hides the others.
func (a *app) showPage(page int) {
	a.page = page
	for _, c := range a.homeControls() {
		setShown(c, page == pageHome)
	}
	a.applyOpVisibility()
	if page == pageHome {
		a.refreshHome() // restores the actions' enabled state
	}
	a.layout()
}

// setShown shows or hides a control. A hidden control is also disabled, so
// the dialog manager skips it: otherwise its access key (e.g. Alt+B of the
// hidden "Create backup") would win over the visible one.
func setShown(c win32.HWND, shown bool) {
	win32.SetVisible(c, shown)
	win32.Enable(c, shown)
}

// applyOpVisibility shows the operation screen's controls that the page,
// the content mode, and the buttons call for.
func (a *app) applyOpVisibility() {
	on := a.page == pageOperation
	o := &a.op
	c := a.opContent
	setShown(o.title, on)
	setShown(o.detail, on)
	setShown(o.progress, on && a.opShowProgress)
	setShown(o.report, on && (c == contentReport || c == contentReportAndLog))
	setShown(o.log, on && (c == contentLog || c == contentReportAndLog))
	setShown(o.tree, on && c == contentTree)
	for _, d := range []win32.HWND{o.destLabel, o.destEdit, o.destBrowse, o.destCheck, o.destNote} {
		setShown(d, on && c == contentDestination)
	}
	for i, b := range o.buttons {
		setShown(b, on && i < len(a.opButtons))
	}
}

func (a *app) createHome() error {
	h := &a.home
	var err error
	static := func(text string, style uint32) win32.HWND {
		if err != nil {
			return 0
		}
		var c win32.HWND
		c, err = win32.CreateWindow(0, "STATIC", text, win32.WS_CHILD|win32.WS_VISIBLE|win32.SS_NOPREFIX|style, 0, 0, 0, 0, a.hwnd, 0)
		return c
	}
	button := func(text string, id uintptr) win32.HWND {
		if err != nil {
			return 0
		}
		var c win32.HWND
		c, err = win32.CreateWindow(0, "BUTTON", text, win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.BS_PUSHBUTTON, 0, 0, 0, 0, a.hwnd, id)
		return c
	}

	// Creation order is the tab order.
	h.configLabel = static("Configuration", win32.SS_CENTERIMAGE)
	h.configPath = static(filepath.Clean(a.opts.ConfigPath), win32.SS_CENTERIMAGE|win32.SS_PATHELLIPSIS)
	h.configOpen = button("Open &file", idConfigOpen)
	h.backupLabel = static("Backups", win32.SS_CENTERIMAGE)
	h.backupPath = static(filepath.Clean(a.backupDir), win32.SS_CENTERIMAGE|win32.SS_PATHELLIPSIS)
	h.backupOpen = button("Open f&older", idBackupOpen)
	if err == nil {
		h.report, err = win32.CreateWindow(0, win32.MSFTEDIT_CLASS, "Startup health check",
			win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL,
			0, 0, 0, 0, a.hwnd, 0)
	}
	h.status = static("", win32.SS_LEFT)
	h.backup = button("Create &backup", idBackup)
	h.restore = button("&Restore backup", idRestore)
	h.verify = button("&Verify backup", idVerify)
	h.recheck = button("Rechec&k", idRecheck)
	if err != nil {
		return err
	}
	win32.SendMessage(h.report, win32.EM_SETBKGNDCOLOR, 0, uintptr(win32.SysColor(win32.COLOR_WINDOW)))
	return nil
}

// layout positions the controls of the current page.
func (a *app) layout() {
	if a.home.report == 0 || a.op.log == 0 {
		return
	}
	client := win32.ClientRect(a.hwnd)
	s := widget.Scale(a.dpi)
	if a.page == pageOperation {
		l := layoutOperation(s, client.Width(), client.Height(), a.opContent, a.opShowProgress)
		o := &a.op
		for c, r := range map[win32.HWND]win32.Rect{o.title: l.title, o.detail: l.detail, o.progress: l.progress, o.report: l.report, o.log: l.log, o.tree: l.tree,
			o.destLabel: l.destLabel, o.destEdit: l.destEdit, o.destBrowse: l.destBrowse, o.destCheck: l.destCheck, o.destNote: l.destNote} {
			win32.SetWindowPos(c, r)
		}
		for i, b := range o.buttons {
			win32.SetWindowPos(b, l.buttons[i])
		}
		return
	}
	l := layoutHome(s, client.Width(), client.Height())
	h := &a.home
	for c, r := range map[win32.HWND]win32.Rect{
		h.configLabel: l.configLabel, h.configPath: l.configPath, h.configOpen: l.configOpen,
		h.backupLabel: l.backupLabel, h.backupPath: l.backupPath, h.backupOpen: l.backupOpen,
		h.report: l.report, h.status: l.blocked,
		h.backup: l.backup, h.restore: l.restore, h.verify: l.verify, h.recheck: l.recheck,
	} {
		win32.SetWindowPos(c, r)
	}
}

// startHealthCheck runs the startup health check in the background; it can
// take a while on a network drive, and the window stays responsive.
func (a *app) startHealthCheck() {
	a.state.checking = true
	a.refreshHome()
	go func() {
		result := health.Check(a.opts.Config, a.opts.ExeDir, a.opts.ConfigPath)
		a.mu.Lock()
		a.pendingHealth = &result
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgHealthDone, 0, 0) //nolint:errcheck
	}()
}

func (a *app) healthDone() {
	a.mu.Lock()
	result := a.pendingHealth
	a.pendingHealth = nil
	a.mu.Unlock()
	if result == nil {
		return
	}
	a.state = homeState{health: result}
	a.refreshHome()
	if a.page == pageHome {
		a.focusHome()
	}
}

func (a *app) refreshHome() {
	h := &a.home
	report := interact.Report{Title: "Startup health check", Sections: []interact.Section{{Rows: []interact.Row{interact.Note("Checking ...")}}}}
	if a.state.health != nil && !a.state.checking {
		report = a.state.health.Report()
	}
	win32.SetRichText(h.report, reportRTF(report, a.fontFace, a.fontPt))
	backup, restoreOrVerify := a.state.actionsEnabled()
	win32.Enable(h.backup, backup)
	win32.Enable(h.restore, restoreOrVerify)
	win32.Enable(h.verify, restoreOrVerify)
	win32.Enable(h.recheck, !a.state.checking)
	win32.SetText(h.status, a.state.statusLine())
}

func (a *app) onCommand(id uint16) {
	// Commands of the page that is not shown are ignored (see setShown).
	if (a.page == pageHome) != (id < idOpButton) {
		return
	}
	switch id {
	case idConfigOpen:
		a.open(a.opts.ConfigPath, true)
	case idBackupOpen:
		a.open(a.backupDir, false)
	case idRecheck:
		a.startHealthCheck()
	case idBackup:
		a.startOperation(opBackup)
	case idRestore:
		a.startOperation(opRestore)
	case idVerify:
		a.startOperation(opVerify)
	default:
		if i := int(id) - idOpButton; i >= 0 && i < len(a.opButtons) {
			a.opButtons[i].onClick()
		}
	}
}

// open opens path with its default application; a file without one (e.g.
// .yaml) opens in Notepad.
func (a *app) open(path string, isFile bool) {
	err := win32.ShellOpen(a.hwnd, path)
	if err != nil && isFile {
		err = win32.ShellOpenWith(a.hwnd, "notepad.exe", `"`+path+`"`)
	}
	if err != nil {
		win32.MessageBox(a.hwnd, fmt.Sprintf("Cannot open %s: %v", filepath.ToSlash(path), err), "RestoreSafe", win32.MB_OK|win32.MB_ICONERROR)
	}
}

func wndProc(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	a := theApp
	switch msg {
	case win32.WM_GETMINMAXINFO:
		s := widget.Scale(win32.DpiForWindow(hwnd))
		minRect := win32.WindowRectForClient(win32.Rect{Right: s.Px(windowMinWidth), Bottom: s.Px(windowMinHeight)}, windowStyle, windowExStyle, uint32(s))
		win32.MinMaxInfoParam(lparam).MinTrackSize = win32.Point{X: minRect.Width(), Y: minRect.Height()}
		return 0
	}
	if a == nil || a.hwnd == 0 || hwnd != a.hwnd {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case win32.WM_SIZE:
		a.layout()
		return 0
	case win32.WM_DPICHANGED:
		a.dpi = uint32(win32.HiWord(wparam))
		a.createFonts() //nolint:errcheck // keeps the previous font on failure
		win32.SetWindowPos(hwnd, *win32.RectParam(lparam))
		a.layout()
		a.refreshHome()
		if a.opReport != nil {
			win32.SetRichText(a.op.report, reportRTF(*a.opReport, a.fontFace, a.fontPt))
		}
		return 0
	case win32.WM_CTLCOLORSTATIC:
		win32.SetBkModeTransparent(wparam)
		if win32.HWND(lparam) == a.op.title {
			if _, color := statusMarker(a.opTitleStatus); color != 0 {
				win32.SetTextColor(wparam, rtfColors[color])
			}
		}
		return uintptr(win32.SysColorBrush(win32.COLOR_WINDOW))
	case win32.WM_COMMAND:
		id, code := win32.LoWord(wparam), win32.HiWord(wparam)
		switch {
		case id == win32.IDCANCEL && a.page == pageOperation:
			a.clickCancel() // Esc
			return 0
		case id == idDestEdit && code == win32.EN_CHANGE:
			a.updateDestination()
			return 0
		case id == idDestCheck && code == win32.BN_CLICKED:
			a.updateDestination()
			return 0
		case id == idDestBrowse && code == win32.BN_CLICKED:
			a.browseDestination()
			return 0
		case code == win32.BN_CLICKED && lparam != 0:
			a.onCommand(id)
			return 0
		}
	case win32.DM_GETDEFID:
		// Enter clicks the first button of the operation screen.
		if a.page == pageOperation && len(a.opButtons) > 0 {
			return win32.DC_HASDEFID<<16 | uintptr(idOpButton)
		}
		return 0
	case win32.WM_NOTIFY:
		if nm := win32.NMHdrParam(lparam); nm.HwndFrom == a.op.tree {
			switch nm.Code {
			case win32.TVN_SELCHANGEDW:
				a.onTreeSelection()
			case win32.NM_DBLCLK, win32.NM_RETURN:
				a.clickDefault()
			}
		}
		return 0
	case msgHealthDone:
		a.healthDone()
		return 0
	case msgBridge:
		if a.run != nil {
			switch wparam {
			case flow.NoteQuestion:
				a.run.b.ShowNext()
			case flow.NoteOutput:
				a.onOutput()
			case flow.NoteProgress:
				a.onProgress()
			}
		}
		return 0
	case msgWorkerDone:
		a.onWorkerDone()
		return 0
	case win32.WM_TIMER:
		if wparam == elapsedTimerID {
			a.updateElapsed()
		}
		return 0
	case win32.WM_CLOSE:
		a.onClose()
		return 0
	case win32.WM_QUERYENDSESSION:
		if a.onQueryEndSession() {
			return 1
		}
		return 0
	case win32.WM_ENDSESSION:
		if wparam != 0 {
			a.onEndSession()
		}
		return 0
	case win32.WM_DESTROY:
		for _, f := range []windows.Handle{a.font, a.boldFont, a.monoFont} {
			win32.DeleteObject(f)
		}
		win32.PostQuitMessage(0)
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// appIcon loads the application icon in the given size.
func appIcon(size int32) windows.Handle {
	if icon := win32.LoadIcon(appIconID, size); icon != 0 {
		return icon
	}
	return win32.LoadIcon(appIconIDFallback, size)
}

// setAccessibleNames names the controls screen readers cannot name from a
// label: the rich edits (their window text is the content), the tree, and
// the progress bar.
func (a *app) setAccessibleNames() {
	for hwnd, name := range map[win32.HWND]string{
		a.home.report: "Startup health check",
		a.op.report:   "Preflight summary",
		a.op.log:      "Log",
		a.op.tree:     "Backups",
		a.op.progress: "Progress",
	} {
		win32.SetAccessibleName(hwnd, name)
	}
}

// focusHome puts the keyboard focus on the first home action that can run,
// or on Recheck.
func (a *app) focusHome() {
	h := &a.home
	for _, b := range []win32.HWND{h.backup, h.restore, h.verify, h.recheck} {
		if win32.IsEnabled(b) {
			win32.SetFocus(b)
			return
		}
	}
}
