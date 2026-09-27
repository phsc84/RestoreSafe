// Package gui is the graphical frontend of RestoreSafe: a native Win32 main
// window with screens and dialogs (see docs/SPEC-restoresafe-gui.md).
package gui

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/startup"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"RestoreSafe/internal/win32"
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
	Config     *util.Config
}

// Control IDs and application messages.
const (
	idConfigOpen = 101 + iota
	idBackupOpen
	idBackup
	idRestore
	idVerify
	idRecheck

	msgHealthDone = win32.WM_APP + 1
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
	fontFace string
	fontPt   int

	home struct {
		configLabel, configPath, configOpen win32.HWND
		backupLabel, backupPath, backupOpen win32.HWND
		report, status                      win32.HWND
		backup, restore, verify, recheck    win32.HWND
	}
	state homeState

	mu            sync.Mutex
	pendingHealth *startup.HealthCheckResult
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
	if err := win32.LoadRichEdit(); err != nil {
		return err
	}
	a := &app{opts: opts, backupDir: util.ResolveDir(opts.Config.BackupDirectory, opts.ExeDir)}
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
	s := scale(dpi)
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
	frame := win32.WindowRectForClient(win32.Rect{Right: s.px(windowWidth), Bottom: s.px(windowHeight)}, windowStyle, windowExStyle, dpi)
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
	security.SetParentWindow(uintptr(hwnd))

	if err := a.createFonts(); err != nil {
		return err
	}
	if err := a.createHome(); err != nil {
		return err
	}
	a.layout()
	win32.ShowWindow(hwnd, win32.SW_SHOWNORMAL)
	return nil
}

// createFonts creates the message font for the current DPI and applies it
// to every control; the previous font is deleted afterwards.
func (a *app) createFonts() error {
	lf, err := win32.MessageFont(a.dpi)
	if err != nil {
		return err
	}
	font, err := win32.CreateFont(&lf)
	if err != nil {
		return err
	}
	old := a.font
	a.font = font
	a.fontFace = lf.Face()
	a.fontPt = max(int((-lf.Height*72+int32(a.dpi)/2)/int32(a.dpi)), 8)
	for _, c := range a.controls() {
		win32.SetFont(c, font)
	}
	win32.DeleteObject(old)
	a.applyRichEditZoom()
	return nil
}

// applyRichEditZoom scales the rich edit's content to the window's DPI (the
// control lays out RTF point sizes at the system DPI) and sets its inner
// margins.
func (a *app) applyRichEditZoom() {
	if a.home.report == 0 {
		return
	}
	win32.SendMessage(a.home.report, win32.EM_SETZOOM, uintptr(a.dpi), uintptr(win32.DpiForSystem()))
	pad := uintptr(scale(a.dpi).px(reportPadding))
	win32.SendMessage(a.home.report, win32.EM_SETMARGINS, win32.EC_LEFTMARGIN|win32.EC_RIGHTMARGIN, pad|pad<<16)
}

func (a *app) controls() []win32.HWND {
	h := &a.home
	all := []win32.HWND{h.configLabel, h.configPath, h.configOpen, h.backupLabel, h.backupPath, h.backupOpen, h.report, h.status, h.backup, h.restore, h.verify, h.recheck}
	out := all[:0]
	for _, c := range all {
		if c != 0 {
			out = append(out, c)
		}
	}
	return out
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
	for _, c := range a.controls() {
		win32.SetFont(c, a.font)
	}
	a.applyRichEditZoom()
	return nil
}

func (a *app) layout() {
	if a.home.report == 0 {
		return
	}
	client := win32.ClientRect(a.hwnd)
	l := layoutHome(scale(a.dpi), client.Width(), client.Height())
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
		result := startup.CheckHealth(a.opts.Config, a.opts.ExeDir, a.opts.ConfigPath)
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
}

func (a *app) refreshHome() {
	h := &a.home
	report := ui.Report{Title: "Startup health check", Sections: []ui.Section{{Rows: []ui.Row{ui.Note("Checking ...")}}}}
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
	switch id {
	case idConfigOpen:
		a.open(a.opts.ConfigPath, true)
	case idBackupOpen:
		a.open(a.backupDir, false)
	case idRecheck:
		a.startHealthCheck()
	case idBackup, idRestore, idVerify:
		win32.MessageBox(a.hwnd, "Running backups, restores, and verifications from the window is being built (GUI phase G3).", "RestoreSafe", win32.MB_OK)
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
		s := scale(win32.DpiForWindow(hwnd))
		minRect := win32.WindowRectForClient(win32.Rect{Right: s.px(windowMinWidth), Bottom: s.px(windowMinHeight)}, windowStyle, windowExStyle, uint32(s))
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
		return 0
	case win32.WM_CTLCOLORSTATIC:
		win32.SetBkModeTransparent(wparam)
		return uintptr(win32.SysColorBrush(win32.COLOR_WINDOW))
	case win32.WM_COMMAND:
		if win32.HiWord(wparam) == win32.BN_CLICKED && lparam != 0 {
			a.onCommand(win32.LoWord(wparam))
			return 0
		}
	case msgHealthDone:
		a.healthDone()
		return 0
	case win32.WM_CLOSE:
		win32.DestroyWindow(hwnd)
		return 0
	case win32.WM_DESTROY:
		win32.DeleteObject(a.font)
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
