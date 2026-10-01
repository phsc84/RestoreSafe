// Package gui is the graphical frontend of RestoreSafe: a native Win32 main
// window with a sidebar, pages and dialogs (see docs/SPEC-restoresafe-gui.md).
// What the pages show is computed by gui/view; the pages render it with the
// controls of gui/widget.
package gui

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// Options configure the GUI.
type Options struct {
	Version    string
	ExeDir     string
	ConfigPath string
	Config     *config.Config
}

// Control IDs and application messages. The IDs are the AutomationIds of
// the controls (spec 15).
const (
	idOpButton = 120 // idOpButton+i is button i of the operation screen

	idDestEdit   = 202
	idDestBrowse = 203
	idDestCheck  = 204

	idSidebar = 301
	idStatus  = 302
	idPage    = 310 // idPage+page is the panel of a page

	msgSnapshot   = win32.WM_APP + 1
	msgBridge     = win32.WM_APP + 2 // wparam: flow.NoteQuestion, NoteOutput, NoteProgress
	msgWorkerDone = win32.WM_APP + 3
	msgLogLoaded  = win32.WM_APP + 4 // a log file for the Backups page was read
	msgListFocus  = win32.WM_APP + 5 // the Backups list may have moved the focus to a group
)

// Pages of the main window: the pages of the navigation (view.Page*) and,
// while an operation runs, the operation screen of the first GUI.
const pageOperation = 3

const (
	windowClass   = "RestoreSafeMainWindow"
	windowStyle   = win32.WS_OVERLAPPEDWINDOW | win32.WS_CLIPCHILDREN
	windowExStyle = win32.WS_EX_CONTROLPARENT
	// goversioninfo numbers resources in order: the manifest gets ID 1 and
	// the icon group ID 2; without a manifest the icon group is ID 1.
	appIconID         = 2
	appIconIDFallback = 1
)

// recheckAfter is how old a snapshot may get before activating the window
// checks again (spec OV-8).
const recheckAfter = 5 * time.Minute

// app is the main window. There is one per process; the window procedure
// reaches it through theApp.
type app struct {
	opts      Options
	backupDir string

	hwnd  win32.HWND
	dpi   uint32
	theme *widget.Theme
	page  int
	modal win32.HWND  // open credential dialog, if any
	plan  *planDialog // open backup plan dialog, if any

	// taskbar shows the operation on the taskbar button once it exists;
	// taskbarCreated is the message that says so.
	taskbar        *win32.Taskbar
	taskbarCreated uint32
	// logText is what the operation wrote, for "Show log".
	logText strings.Builder
	// verifyWhat names the verified selection in the confirmation.
	verifyWhat string

	// The shell of the new interface.
	shell shell

	// The first GUI's message fonts, for the operation screen and its
	// dialogs (replaced in plan phases 6 to 8).
	font     windows.Handle
	boldFont windows.Handle
	monoFont windows.Handle
	fontFace string
	fontPt   int

	op struct {
		title, detail, progress win32.HWND
		report, log             win32.HWND
		destLabel, destEdit     win32.HWND
		destBrowse, destCheck   win32.HWND
		destNote                win32.HWND
		buttons                 [opButtons]win32.HWND
	}
	opButtons      []opButton
	opTitleStatus  interact.Status
	opContent      opContent
	opShowProgress bool
	// Destination screen.
	destDefault  string // the backup directory, for "restore into the backup directory"
	progressText string
	machine      flow.Machine     // the stage of the operation
	run          *runState        // its worker, nil when none runs
	opReport     *interact.Report // preflight report on screen, re-rendered on DPI changes

	// The state of the backups.
	checker    health.Checker
	snapshot   *health.Snapshot
	checking   bool
	mu         sync.Mutex
	pendingSn  *health.Snapshot
	pendingLog *loadedLog
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
	a.startCheck()

	var msg win32.Msg
	for {
		ok, err := win32.GetMessage(&msg)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if a.plan != nil && win32.IsDialogMessage(a.plan.win.hwnd, &msg) {
			continue
		}
		if msg.Message == win32.WM_KEYDOWN && a.shortcut(msg.WParam) {
			continue
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

// title returns the window title (spec 3.2).
func (a *app) title() string {
	return view.Title(a.opts.Version, filepath.Base(a.opts.ConfigPath))
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
	frame := win32.WindowRectForClient(win32.Rect{Right: s.Px(widget.WindowWidth), Bottom: s.Px(widget.WindowHeight)}, windowStyle, windowExStyle, dpi)
	w := min(frame.Width(), work.Width())
	h := min(frame.Height(), work.Height())
	x := work.Left + (work.Width()-w)/2
	y := work.Top + (work.Height()-h)/2

	a.taskbarCreated = win32.TaskbarButtonCreated()
	hwnd, err := win32.CreateWindow(windowExStyle, windowClass, a.title(), windowStyle, x, y, w, h, 0, 0)
	if err != nil {
		return err
	}
	a.hwnd = hwnd
	a.dpi = win32.DpiForWindow(hwnd)
	yubikey.SetParentWindow(uintptr(hwnd))

	fonts, err := widget.NewFonts(widget.Scale(a.dpi))
	if err != nil {
		return err
	}
	a.theme = &widget.Theme{Palette: widget.Light, Fonts: fonts, Scale: widget.Scale(a.dpi)}
	if err := a.createFonts(); err != nil {
		return err
	}
	if err := a.createShell(); err != nil {
		return err
	}
	if err := a.createOperation(); err != nil {
		return err
	}
	a.applyFonts()
	a.setAccessibleNames()
	a.showPage(view.PageOverview)
	win32.ShowWindow(hwnd, win32.SW_SHOWNORMAL)
	return nil
}

// createFonts creates the message font, a bold variant for headings, and a
// monospaced font for the log of the operation screen, for the current
// DPI, and applies them; the previous fonts are deleted afterwards.
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

// applyFonts sets the fonts of the operation screen's controls and the rich
// edits' zoom and margins for the current DPI.
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
	for _, re := range []win32.HWND{a.op.report, a.op.log} {
		if re == 0 {
			continue
		}
		// The rich edit lays out RTF point sizes at the system DPI.
		win32.SendMessage(re, win32.EM_SETZOOM, uintptr(a.dpi), uintptr(win32.DpiForSystem()))
		win32.SendMessage(re, win32.EM_SETMARGINS, win32.EC_LEFTMARGIN|win32.EC_RIGHTMARGIN, pad|pad<<16)
	}
}

// controls returns the operation screen's controls that use the message font.
func (a *app) controls() []win32.HWND {
	all := []win32.HWND{a.op.detail, a.op.report, a.op.destLabel, a.op.destEdit, a.op.destBrowse, a.op.destCheck, a.op.destNote}
	all = append(all, a.op.buttons[:]...)
	out := all[:0]
	for _, c := range all {
		if c != 0 {
			out = append(out, c)
		}
	}
	return out
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

// setShown shows or hides a control. A hidden control is also disabled, so
// the dialog manager skips it: otherwise its access key (e.g. Alt+B of the
// hidden "Back up now") would win over the visible one.
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
	for _, d := range []win32.HWND{o.destLabel, o.destEdit, o.destBrowse, o.destCheck, o.destNote} {
		setShown(d, on && c == contentDestination)
	}
	for i, b := range o.buttons {
		setShown(b, on && i < len(a.opButtons))
	}
}

// layout positions the shell or the operation screen.
func (a *app) layout() {
	if a.op.log == 0 || a.shell.sidebar == nil {
		return
	}
	client := win32.ClientRect(a.hwnd)
	if a.page != pageOperation {
		a.layoutShell(client)
		return
	}
	s := widget.Scale(a.dpi)
	l := layoutOperation(s, client.Width(), client.Height(), a.opContent, a.opShowProgress)
	o := &a.op
	for c, r := range map[win32.HWND]win32.Rect{o.title: l.title, o.detail: l.detail, o.progress: l.progress, o.report: l.report, o.log: l.log,
		o.destLabel: l.destLabel, o.destEdit: l.destEdit, o.destBrowse: l.destBrowse, o.destCheck: l.destCheck, o.destNote: l.destNote} {
		win32.SetWindowPos(c, r)
	}
	for i, b := range o.buttons {
		win32.SetWindowPos(b, l.buttons[i])
	}
}

// startCheck takes a new snapshot in the background; it can take a while
// on a network drive, and the window stays responsive. A check that runs
// longer than health.SnapshotTimeout reports the backup directory as not
// responding.
func (a *app) startCheck() {
	if a.checking {
		return
	}
	a.checking = true
	a.refreshShell()
	params := health.Params{Config: a.opts.Config, ExeDir: a.opts.ExeDir, ConfigPath: a.opts.ConfigPath, Now: time.Now()}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), health.SnapshotTimeout)
		defer cancel()
		s := a.checker.Snapshot(ctx, params)
		a.mu.Lock()
		a.pendingSn = &s
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgSnapshot, 0, 0) //nolint:errcheck
	}()
}

// snapshotDone shows the snapshot the check posted.
func (a *app) snapshotDone() {
	a.mu.Lock()
	s := a.pendingSn
	a.pendingSn = nil
	a.mu.Unlock()
	if s == nil {
		return
	}
	first := a.snapshot == nil
	a.snapshot = s
	a.checking = false
	a.refreshShell()
	// After the first check, the keyboard starts at the hero's action.
	if first && a.page == view.PageOverview {
		a.focusPage()
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
		minRect := win32.WindowRectForClient(win32.Rect{Right: s.Px(widget.WindowMinWidth), Bottom: s.Px(widget.WindowMinHeight)}, windowStyle, windowExStyle, uint32(s))
		win32.MinMaxInfoParam(lparam).MinTrackSize = win32.Point{X: minRect.Width(), Y: minRect.Height()}
		return 0
	}
	if a == nil || a.hwnd == 0 || hwnd != a.hwnd {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	if a.taskbarCreated != 0 && msg == a.taskbarCreated {
		a.taskbar.Release()
		a.taskbar, _ = win32.NewTaskbar(hwnd) //nolint:errcheck // the taskbar shows no progress then
		a.updateTaskbar()
		return 0
	}
	switch msg {
	case win32.WM_SIZE:
		a.layout()
		return 0
	case win32.WM_DPICHANGED:
		a.dpi = uint32(win32.HiWord(wparam))
		a.createFonts() //nolint:errcheck // keeps the previous font on failure
		a.restyle()
		win32.SetWindowPos(hwnd, *win32.RectParam(lparam))
		a.layout()
		if a.opReport != nil {
			win32.SetRichText(a.op.report, reportRTF(*a.opReport, a.fontFace, a.fontPt))
		}
		return 0
	case win32.WM_ACTIVATEAPP:
		if wparam != 0 && a.run == nil && a.snapshot != nil && time.Since(a.snapshot.Checked) > recheckAfter {
			a.startCheck()
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
		case id == win32.IDOK && a.page == view.PageBackups && win32.Focus() == a.shell.backups.list:
			// Enter in the list restores the selection (spec BK-4).
			if a.shell.backups.bar.Restore.Enabled {
				a.do(view.ActionRestore)
			}
			return 0
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
		case code == win32.BN_CLICKED && lparam != 0 && a.page == pageOperation:
			if i := int(id) - idOpButton; i >= 0 && i < len(a.opButtons) {
				a.opButtons[i].onClick()
			}
			return 0
		}
	case win32.DM_GETDEFID:
		// Enter clicks the first button of the operation screen.
		if a.page == pageOperation && len(a.opButtons) > 0 {
			return win32.DC_HASDEFID<<16 | uintptr(idOpButton)
		}
		return 0
	case msgSnapshot:
		a.snapshotDone()
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
	case msgLogLoaded:
		a.mu.Lock()
		l := a.pendingLog
		a.pendingLog = nil
		a.mu.Unlock()
		a.shell.backups.logLoaded(l)
		return 0
	case msgListFocus:
		a.shell.backups.focusChanged()
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
		a.theme.Fonts.Close()
		a.taskbar.Release()
		win32.PostQuitMessage(0)
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// restyle creates the theme's fonts for the new DPI and applies them.
func (a *app) restyle() {
	s := widget.Scale(a.dpi)
	fonts, err := widget.NewFonts(s)
	if err != nil {
		return
	}
	old := a.theme.Fonts
	a.theme.Fonts, a.theme.Scale = fonts, s
	a.restyleShell()
	old.Close()
}

// appIcon loads the application icon in the given size.
func appIcon(size int32) windows.Handle {
	if icon := win32.LoadIcon(appIconID, size); icon != 0 {
		return icon
	}
	return win32.LoadIcon(appIconIDFallback, size)
}

// setAccessibleNames names the operation screen's controls screen readers
// cannot name from a label: the rich edits (their window text is the
// content) and the progress bar.
func (a *app) setAccessibleNames() {
	for hwnd, name := range map[win32.HWND]string{
		a.op.report:   "Preflight summary",
		a.op.log:      "Log",
		a.op.progress: "Progress",
	} {
		win32.SetAccessibleName(hwnd, name)
	}
}
