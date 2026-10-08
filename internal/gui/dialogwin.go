package gui

import (
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// dialogWindow is the frame of the plan and credential dialogs: a window
// owned by another, filled by a panel in the theme. The dialog manager
// (IsDialogMessage) gives it Tab, Enter and Esc: Enter clicks the default
// button, Esc and the close button send IDCANCEL.
type dialogWindow struct {
	hwnd  win32.HWND
	owner win32.HWND
	panel *widget.Panel
	// theme is the dialog's own copy of the app's theme: a DPI change of
	// the dialog gives it fonts at the new scale (ownFonts) and calls
	// onDpi with the previous scale, after the window took its new size.
	theme    *widget.Theme
	ownFonts bool
	onDpi    func(old widget.Scale)
	// onCommand receives the clicks of the panel's buttons and links and
	// IDCANCEL; defID returns the ID of the default button, 0 for none.
	onCommand func(id uint16)
	defID     func() uint16
	// onMessage sees the window's other messages first; handled stops
	// the default handling.
	onMessage func(msg uint32, wparam, lparam uintptr) (result uintptr, handled bool)
}

const dialogStyle = win32.WS_POPUP | win32.WS_CAPTION | win32.WS_SYSMENU

const dialogExStyle = win32.WS_EX_DLGMODALFRAME | win32.WS_EX_CONTROLPARENT

// newDialogWindow creates a hidden dialog window of class, owned by owner.
func newDialogWindow(t *widget.Theme, owner win32.HWND, class, title string) (*dialogWindow, error) {
	if err := registerClass(win32.WndClassEx{}, class); err != nil {
		return nil, err
	}
	hwnd, err := win32.CreateWindow(dialogExStyle, class, title, dialogStyle, 0, 0, 0, 0, owner, 0)
	if err != nil {
		return nil, err
	}
	own := *t
	d := &dialogWindow{hwnd: hwnd, owner: owner, theme: &own}
	panel, err := widget.NewPanel(d.theme, hwnd, 0, widget.PanelStyle{Back: t.Palette.Surface})
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
	handlers[hwnd] = d
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
	frame := win32.WindowRectForClient(win32.Rect{Right: w, Bottom: h}, win32.Style(d.hwnd), dialogExStyle, dpi)
	r := win32.WindowRect(d.hwnd)
	if place {
		o := win32.WindowRect(d.owner)
		r.Left = o.Left + (o.Width()-frame.Width())/2
		r.Top = o.Top + max((o.Height()-frame.Height())/3, 0)
	}
	win32.SetWindowPos(d.hwnd, win32.Rect{Left: r.Left, Top: r.Top, Right: r.Left + frame.Width(), Bottom: r.Top + frame.Height()})
	win32.SetWindowPos(d.panel.HWND(), win32.Rect{Right: w, Bottom: h})
}

// dpiChanged gives the dialog fonts at scale s, takes the size Windows
// suggests and lets the dialog lay itself out again.
func (d *dialogWindow) dpiChanged(s widget.Scale, suggested win32.Rect) {
	old := d.theme.Scale
	if s == old {
		return
	}
	fonts, err := widget.NewFonts(s)
	if err != nil {
		return
	}
	previous, owned := d.theme.Fonts, d.ownFonts
	d.theme.Fonts, d.theme.Scale, d.ownFonts = fonts, s, true
	win32.SetWindowPos(d.hwnd, suggested)
	win32.SetWindowPos(d.panel.HWND(), win32.ClientRect(d.hwnd))
	d.panel.Restyle()
	if d.onDpi != nil {
		d.onDpi(old)
	}
	if owned {
		previous.Close()
	}
}

// destroy closes the dialog and gives the activation back to the owner,
// which the caller enabled again first.
func (d *dialogWindow) destroy() {
	if d.ownFonts {
		defer d.theme.Fonts.Close()
	}
	delete(handlers, d.hwnd)
	win32.SetForeground(d.owner)
	win32.DestroyWindow(d.hwnd)
}

// message handles the messages of the dialog window.
func (d *dialogWindow) message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	if d.onMessage != nil {
		if r, ok := d.onMessage(msg, wparam, lparam); ok {
			return r
		}
	}
	switch msg {
	case win32.WM_DPICHANGED:
		d.dpiChanged(widget.Scale(win32.HiWord(wparam)), *win32.RectParam(lparam))
		return 0
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
