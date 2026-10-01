package gui

import (
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"

	"golang.org/x/sys/windows"
)

// dialogWindow is the frame of the plan and credential dialogs: a window
// owned by another, filled by a panel in the theme. The dialog manager
// (IsDialogMessage) gives it Tab, Enter and Esc: Enter clicks the default
// button, Esc and the close button send IDCANCEL.
type dialogWindow struct {
	hwnd  win32.HWND
	owner win32.HWND
	panel *widget.Panel
	// onCommand receives the clicks of the panel's buttons and links and
	// IDCANCEL; defID returns the ID of the default button, 0 for none.
	onCommand func(id uint16)
	defID     func() uint16
}

var (
	dialogWindows      = map[win32.HWND]*dialogWindow{}
	dialogClassesExist = map[string]bool{}
	dialogProcPtr      uintptr
)

const dialogStyle = win32.WS_POPUP | win32.WS_CAPTION | win32.WS_SYSMENU

const dialogExStyle = win32.WS_EX_DLGMODALFRAME | win32.WS_EX_CONTROLPARENT

// newDialogWindow creates a hidden dialog window of class, owned by owner.
func newDialogWindow(t *widget.Theme, owner win32.HWND, class, title string) (*dialogWindow, error) {
	if !dialogClassesExist[class] {
		if dialogProcPtr == 0 {
			dialogProcPtr = windows.NewCallback(dialogWindowProc)
		}
		wc := win32.WndClassEx{
			WndProc:    dialogProcPtr,
			Instance:   win32.ModuleHandle(),
			Cursor:     win32.ArrowCursor(),
			Background: win32.SysColorBrush(win32.COLOR_WINDOW),
			ClassName:  win32.UTF16(class),
		}
		if err := win32.RegisterClass(&wc); err != nil {
			return nil, err
		}
		dialogClassesExist[class] = true
	}
	hwnd, err := win32.CreateWindow(dialogExStyle, class, title, dialogStyle, 0, 0, 0, 0, owner, 0)
	if err != nil {
		return nil, err
	}
	d := &dialogWindow{hwnd: hwnd, owner: owner}
	panel, err := widget.NewPanel(t, hwnd, 0, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		win32.DestroyWindow(hwnd)
		return nil, err
	}
	d.panel = panel
	panel.OnCommand = func(id, code uint16) {
		if code == win32.BN_CLICKED || code == 0 {
			d.command(id)
		}
	}
	dialogWindows[hwnd] = d
	return d, nil
}

func (d *dialogWindow) command(id uint16) {
	if d.onCommand != nil {
		d.onCommand(id)
	}
}

// resize gives the dialog a client area of w×h pixels; place centers it
// over the owner, otherwise it keeps its position.
func (d *dialogWindow) resize(w, h int32, place bool) {
	dpi := win32.DpiForWindow(d.hwnd)
	frame := win32.WindowRectForClient(win32.Rect{Right: w, Bottom: h}, dialogStyle, dialogExStyle, dpi)
	r := win32.WindowRect(d.hwnd)
	if place {
		o := win32.WindowRect(d.owner)
		r.Left = o.Left + (o.Width()-frame.Width())/2
		r.Top = o.Top + max((o.Height()-frame.Height())/3, 0)
	}
	win32.SetWindowPos(d.hwnd, win32.Rect{Left: r.Left, Top: r.Top, Right: r.Left + frame.Width(), Bottom: r.Top + frame.Height()})
	win32.SetWindowPos(d.panel.HWND(), win32.Rect{Right: w, Bottom: h})
}

// destroy closes the dialog and gives the activation back to the owner,
// which the caller enabled again first.
func (d *dialogWindow) destroy() {
	delete(dialogWindows, d.hwnd)
	win32.SetForeground(d.owner)
	win32.DestroyWindow(d.hwnd)
}

func dialogWindowProc(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	d := dialogWindows[hwnd]
	if d == nil {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case win32.WM_COMMAND:
		d.command(win32.LoWord(wparam))
		return 0
	case win32.WM_CLOSE:
		d.command(win32.IDCANCEL)
		return 0
	case win32.DM_GETDEFID:
		if d.defID != nil {
			if id := d.defID(); id != 0 {
				return win32.DC_HASDEFID<<16 | uintptr(id)
			}
		}
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}
