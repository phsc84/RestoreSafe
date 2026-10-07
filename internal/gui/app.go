// Package gui is the graphical frontend of RestoreSafe: a native Win32 main
// window with a sidebar, pages and dialogs (see docs/SPEC-gui.md).
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
// the controls (GUI spec 15).
const (
	idSidebar = 301
	idStatus  = 302
	idPage    = 310 // idPage+page is the panel of a page

	msgSnapshot    = win32.WM_APP + 1
	msgBridge      = win32.WM_APP + 2 // wparam: flow.NoteQuestion, NoteOutput, NoteProgress
	msgWorkerDone  = win32.WM_APP + 3
	msgRunLog      = win32.WM_APP + 4 // wparam: the backups list group whose Show log was clicked
	msgListFocus   = win32.WM_APP + 5 // the backups list may have moved the focus to a group
	msgDestChecked = win32.WM_APP + 6 // the Restore window's check of the choices is done
	msgReloaded    = win32.WM_APP + 7 // the configuration file was read again
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

// recheckAfter is how old a snapshot may get before activating the window
// checks again (GUI spec OV-8).
const recheckAfter = 5 * time.Minute

// app is the main window. There is one per process; the window procedure
// reaches it through theApp.
type app struct {
	opts      Options
	backupDir string

	hwnd    win32.HWND
	dpi     uint32
	theme   *widget.Theme
	page    int
	modal   win32.HWND     // open credential dialog, if any
	plan    *planDialog    // open backup plan dialog, if any
	restore *restoreDialog // open Restore window, if any
	verify  *verifyDialog  // open Verify window, if any
	// lastDestination is where the last restore of this session went.
	lastDestination string
	// reloading is set while the configuration file is read again;
	// reloadErr is why it did not load; deferredConfig waits for the
	// running operation; recheck checks again after the running check,
	// which used the previous configuration. addedCopy is the copy of the
	// file that Add to config.yaml saved, until the next Reload.
	reloading      bool
	reloadErr      error
	deferredConfig *config.Config
	recheck        bool
	addedCopy      string

	// taskbar shows the operation on the taskbar button once it exists;
	// taskbarCreated is the message that says so.
	taskbar        *win32.Taskbar
	taskbarCreated uint32
	// logText is what the operation wrote, for "Show log".
	logText strings.Builder
	// verifyWhat names the verified selection; verifyWhole is set when it is
	// every folder of that backup.
	verifyWhat  string
	verifyWhole bool

	// The shell of the new interface.
	shell shell

	// The message font and a monospaced font for the viewers, and the base
	// font of their RTF.
	font     windows.Handle
	monoFont windows.Handle
	fontFace string
	fontPt   int

	machine flow.Machine // the stage of the operation
	run     *runState    // its worker, nil when none runs

	// The state of the backups.
	checker       health.Checker
	snapshot      *health.Snapshot
	checking      bool
	mu            sync.Mutex
	pendingSn     *health.Snapshot
	pendingDest   *destCheck
	pendingReload *reloaded
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
		if a.restore != nil && win32.IsDialogMessage(a.restore.win.hwnd, &msg) {
			continue
		}
		if a.verify != nil && win32.IsDialogMessage(a.verify.win.hwnd, &msg) {
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

// title returns the window title (GUI spec 3.2).
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
	a.theme = &widget.Theme{Palette: widget.CurrentPalette(), Fonts: fonts, Scale: widget.Scale(a.dpi)}
	if err := a.createFonts(); err != nil {
		return err
	}
	if err := a.createShell(); err != nil {
		return err
	}
	a.showPage(view.PageCreate)
	win32.ShowWindow(hwnd, win32.SW_SHOWNORMAL)
	return nil
}

// createFonts creates the message font and a monospaced font for the
// viewers (report and log dialogs) at the current DPI; the previous fonts
// are deleted afterwards.
func (a *app) createFonts() error {
	lf, err := win32.MessageFont(a.dpi)
	if err != nil {
		return err
	}
	font, err := win32.CreateFont(&lf)
	if err != nil {
		return err
	}
	mono := lf
	copy(mono.FaceName[:], windows.StringToUTF16("Consolas"))
	monoFont, err := win32.CreateFont(&mono)
	if err != nil {
		win32.DeleteObject(font)
		return err
	}
	old := []windows.Handle{a.font, a.monoFont}
	a.font, a.monoFont = font, monoFont
	a.fontFace = lf.Face()
	a.fontPt = max(int((-lf.Height*72+int32(a.dpi)/2)/int32(a.dpi)), 8)
	for _, h := range old {
		win32.DeleteObject(h)
	}
	return nil
}

// setShown shows or hides a control. A hidden control is also disabled, so
// the dialog manager skips it: otherwise its access key would win over the
// visible one.
func setShown(c win32.HWND, shown bool) {
	win32.SetVisible(c, shown)
	win32.Enable(c, shown)
}

// layout positions the shell.
func (a *app) layout() {
	if a.shell.sidebar == nil {
		return
	}
	a.layoutShell(win32.ClientRect(a.hwnd))
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
	if a.recheck {
		a.recheck = false
		a.startCheck()
	}
	a.refreshShell()
	// After the first check, the keyboard starts at the hero's action.
	if first && a.page == view.PageCreate {
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
		return 0
	case win32.WM_SETTINGCHANGE, win32.WM_SYSCOLORCHANGE:
		a.paletteChanged()
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	case win32.WM_ACTIVATEAPP:
		if wparam != 0 && a.run == nil && a.snapshot != nil && time.Since(a.snapshot.Checked) > recheckAfter {
			a.startCheck()
		}
		return 0
	case win32.WM_COMMAND:
		if win32.LoWord(wparam) == win32.IDOK && a.page == view.PageRestore && win32.Focus() == a.shell.restore.list {
			// Enter in the list restores the selection (GUI spec BK-4).
			if a.shell.restore.bar.Restore.Enabled {
				a.do(view.ActionRestore)
			}
			return 0
		}
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
	case msgRunLog:
		a.shell.restore.showRunLog(int(int32(wparam)))
		return 0
	case msgListFocus:
		a.shell.restore.focusChanged()
		return 0
	case msgWorkerDone:
		a.onWorkerDone()
		return 0
	case msgReloaded:
		a.reloadDone()
		return 0
	case msgDestChecked:
		a.mu.Lock()
		c := a.pendingDest
		a.pendingDest = nil
		a.mu.Unlock()
		if a.restore != nil {
			a.restore.destChecked(c)
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
		for _, f := range []windows.Handle{a.font, a.monoFont} {
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
