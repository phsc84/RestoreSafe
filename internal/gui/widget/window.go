package widget

import (
	"sync"

	"github.com/phsc84/restoresafe/internal/gui/win32"

	"golang.org/x/sys/windows"
)

// Theme is what widgets draw with: the palette and the fonts at the scale
// of their window. When the DPI changes, the owner replaces Fonts and Scale
// and calls Restyle on its panels.
type Theme struct {
	Palette Palette
	Fonts   *Fonts
	Scale   Scale
}

// control is a widget with its own window class.
type control interface {
	// paint draws the client area r into hdc.
	paint(hdc uintptr, r win32.Rect)
	// message handles a window message; handled is false to let the
	// default window procedure handle it.
	message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) (result uintptr, handled bool)
}

// controls maps the windows of the widgets to them. Widgets live on the UI
// thread only, so the map needs no lock.
var controls = map[win32.HWND]control{}

// creating is the widget whose window is being created: its first messages
// arrive before CreateWindow returns the handle.
var creating control

var (
	classesOnce sync.Once
	classesErr  error
	wndProcPtr  uintptr
)

// Window classes of the widgets.
const (
	classPanel    = "RestoreSafePanel"
	classIcon     = "RestoreSafeIcon"
	classBadge    = "RestoreSafeBadge"
	classBar      = "RestoreSafeBar"
	classSidebar  = "RestoreSafeSidebar"
	classTrail    = "RestoreSafeTrail"
	classSplitter = "RestoreSafeSplitter"
)

func registerClasses() error {
	classesOnce.Do(func() {
		wndProcPtr = windows.NewCallback(wndProc)
		for _, name := range []string{classPanel, classIcon, classBadge, classBar, classSidebar, classTrail, classSplitter} {
			wc := win32.WndClassEx{
				WndProc:   wndProcPtr,
				Instance:  win32.ModuleHandle(),
				Cursor:    win32.ArrowCursor(),
				ClassName: win32.UTF16(name),
			}
			if err := win32.RegisterClass(&wc); err != nil {
				classesErr = err
				return
			}
		}
	})
	return classesErr
}

// create creates the window of c as a child of parent.
func create(c control, class string, parent win32.HWND, style, exStyle uint32, id uintptr) (win32.HWND, error) {
	if err := registerClasses(); err != nil {
		return 0, err
	}
	creating = c
	defer func() { creating = nil }()
	hwnd, err := win32.CreateWindow(exStyle, class, "", win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, parent, id)
	if err != nil {
		return 0, err
	}
	controls[hwnd] = c
	return hwnd, nil
}

func wndProc(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	c := controls[hwnd]
	if c == nil && creating != nil {
		c = creating
		controls[hwnd] = c
	}
	if c == nil {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case win32.WM_ERASEBKGND:
		return 1
	case win32.WM_PAINT:
		paintBuffered(hwnd, c)
		return 0
	case win32.WM_PRINTCLIENT:
		c.paint(wparam, win32.ClientRect(hwnd))
		return 0
	case win32.WM_NCDESTROY:
		delete(controls, hwnd)
	}
	if r, ok := c.message(hwnd, msg, wparam, lparam); ok {
		return r
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// paintBuffered paints c into a memory bitmap and copies it to the window,
// so redrawing never flickers.
func paintBuffered(hwnd win32.HWND, c control) {
	var ps win32.PaintStruct
	hdc := win32.BeginPaint(hwnd, &ps)
	defer win32.EndPaint(hwnd, &ps)
	r := win32.ClientRect(hwnd)
	if r.Width() <= 0 || r.Height() <= 0 {
		return
	}
	mem := win32.CreateCompatibleDC(hdc)
	defer win32.DeleteDC(mem)
	bmp := win32.CreateCompatibleBitmap(hdc, r.Width(), r.Height())
	defer win32.DeleteObject(bmp)
	old := win32.SelectObject(mem, bmp)
	defer win32.SelectObject(mem, old)
	c.paint(mem, r)
	win32.BitBlt(hdc, 0, 0, r.Width(), r.Height(), mem, 0, 0)
}

// fill fills r with color.
func fill(hdc uintptr, r win32.Rect, color Color) {
	brush := win32.CreateSolidBrush(uint32(color))
	win32.FillRect(hdc, r, brush)
	win32.DeleteObject(brush)
}

// roundRect draws r with rounded corners, filled with back and outlined
// with line (line == back draws no visible outline).
func roundRect(hdc uintptr, r win32.Rect, diameter int32, back, line Color) {
	brush := win32.CreateSolidBrush(uint32(back))
	pen := win32.CreatePen(1, uint32(line))
	oldBrush := win32.SelectObject(hdc, brush)
	oldPen := win32.SelectObject(hdc, pen)
	win32.RoundRect(hdc, r, diameter)
	win32.SelectObject(hdc, oldBrush)
	win32.SelectObject(hdc, oldPen)
	win32.DeleteObject(brush)
	win32.DeleteObject(pen)
}

// text draws s in r with style and color, without background.
func text(hdc uintptr, t *Theme, s string, r win32.Rect, style TextStyle, color Color, flags uint32) {
	old := win32.SelectFont(hdc, t.Fonts.Get(style))
	win32.SetBkModeTransparent(hdc)
	win32.SetTextColor(hdc, uint32(color))
	win32.DrawText(hdc, s, r, flags)
	win32.SelectFont(hdc, old)
}

// FillRect fills r of hdc with color: for cells that other controls draw.
func FillRect(hdc uintptr, r win32.Rect, color Color) { fill(hdc, r, color) }
