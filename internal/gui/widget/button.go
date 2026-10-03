package widget

import (
	"RestoreSafe/internal/gui/win32"

	"golang.org/x/sys/windows"
)

// StyleButton paints the native button hwnd (a push button, or a push-like
// radio button) in the theme, on a parent with background back: rounded,
// the accent fill for primary, and the focus outline of the sidebar when
// the keyboard cues are shown. The button keeps its own behavior: Enter,
// Space, arrow keys, its default state and what screen readers announce.
func StyleButton(t *Theme, hwnd win32.HWND, back Color, primary bool) {
	win32.TrackHover(hwnd)
	win32.Subclass(hwnd, func(h win32.HWND, msg uint32, wparam, lparam uintptr, def func() uintptr) uintptr {
		switch msg {
		case win32.WM_ERASEBKGND:
			return 1
		case win32.WM_PAINT:
			var ps win32.PaintStruct
			hdc := win32.BeginPaint(h, &ps)
			paintButtonBuffered(t, h, hdc, back, primary)
			win32.EndPaint(h, &ps)
			return 0
		case win32.WM_PRINTCLIENT:
			paintButton(t, h, wparam, win32.ClientRect(h), back, primary)
			return 0
		case win32.WM_MOUSEMOVE:
			// Dragging out of a pressed button releases it, and back in
			// presses it again.
			before := win32.SendMessage(h, win32.BM_GETSTATE, 0, 0) & win32.BST_PUSHED
			r := def()
			if win32.SendMessage(h, win32.BM_GETSTATE, 0, 0)&win32.BST_PUSHED != before {
				win32.RedrawNow(h)
			}
			return r
		}
		r := def()
		if win32.ButtonStateChange(msg) && win32.IsWindow(h) {
			// The control may have drawn itself; draw over it at once.
			win32.RedrawNow(h)
		}
		return r
	})
}

// paintButtonBuffered paints the button into a memory bitmap first, so it
// never flickers.
func paintButtonBuffered(t *Theme, hwnd win32.HWND, hdc uintptr, back Color, primary bool) {
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
	paintButton(t, hwnd, mem, r, back, primary)
	win32.BitBlt(hdc, 0, 0, r.Width(), r.Height(), mem, 0, 0)
}

// paintButton draws the button hwnd in its current state into hdc.
func paintButton(t *Theme, hwnd win32.HWND, hdc uintptr, r win32.Rect, back Color, primary bool) {
	pal := t.Palette
	state := win32.SendMessage(hwnd, win32.BM_GETSTATE, 0, 0)
	ui := win32.SendMessage(hwnd, win32.WM_QUERYUISTATE, 0, 0)
	pushed := state&win32.BST_PUSHED != 0
	checked := state&win32.BST_CHECKED != 0
	focused := state&win32.BST_FOCUS != 0 && ui&win32.UISF_HIDEFOCUS == 0
	enabled := win32.IsEnabled(hwnd)
	hot := win32.IsHot(hwnd)

	face, line, fore := pal.Surface, pal.Lines, pal.Text
	switch {
	case !enabled && primary:
		face, line, fore = pal.Control, pal.Control, pal.TextSecondary
	case !enabled:
		face, fore = pal.SurfaceAlt, pal.TextSecondary
	case primary:
		face, fore = pal.Accent, pal.OnAccent
		if pushed || hot {
			face = pal.AccentText
		}
		line = face
	case checked:
		face, line = pal.Selection, pal.AccentText
	case pushed:
		face = pal.Control
	case hot:
		face = pal.SurfaceAlt
	}
	if focused {
		line = pal.Text
	}

	fill(hdc, r, back)
	shape := r
	shape.Right--
	shape.Bottom--
	radius := t.Scale.Px(ControlRadius * 2)
	roundRect(hdc, shape, radius, face, line)
	if focused {
		// A second outline inside the first: 2 pixels, as on the sidebar.
		inner := win32.Rect{Left: shape.Left + 1, Top: shape.Top + 1, Right: shape.Right - 1, Bottom: shape.Bottom - 1}
		roundRect(hdc, inner, radius-2, face, line)
	}

	flags := uint32(win32.DT_CENTER | win32.DT_VCENTER | win32.DT_SINGLELINE)
	if ui&win32.UISF_HIDEACCEL != 0 {
		flags |= win32.DT_HIDEPREFIX
	}
	old := win32.SelectFont(hdc, windows.Handle(win32.SendMessage(hwnd, win32.WM_GETFONT, 0, 0)))
	win32.SetBkModeTransparent(hdc)
	win32.SetTextColor(hdc, uint32(fore))
	win32.DrawText(hdc, win32.Text(hwnd), r, flags)
	win32.SelectFont(hdc, old)
}
