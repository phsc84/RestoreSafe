package win32

import "golang.org/x/sys/windows"

// Button state and UI state constants.
const (
	BM_GETSTATE      = 0x00F2
	BM_SETSTATE      = 0x00F3
	BM_SETSTYLE      = 0x00F4
	BST_PUSHED       = 0x0004
	BST_FOCUS        = 0x0008
	WM_QUERYUISTATE  = 0x0129
	UISF_HIDEFOCUS   = 0x1
	UISF_HIDEACCEL   = 0x2
	WM_GETFONT       = 0x0031
	wmEnable         = 0x000A
	wmSetText        = 0x000C
	wmSetFocus       = 0x0007
	wmKillFocus      = 0x0008
	wmKeyDown        = 0x0100
	wmKeyUp          = 0x0101
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmLButtonDblClk  = 0x0203
	wmCaptureChanged = 0x0215
)

// SubclassFunc handles a message of a subclassed control; def runs the
// control's own handling and returns its result.
type SubclassFunc func(hwnd HWND, msg uint32, wparam, lparam uintptr, def func() uintptr) uintptr

var (
	subclassProc uintptr
	subclasses   = map[HWND]SubclassFunc{}
)

// Subclass routes the messages of hwnd through f, until it is destroyed.
func Subclass(hwnd HWND, f SubclassFunc) {
	if subclassProc == 0 {
		subclassProc = windows.NewCallback(subclassCallback)
	}
	subclasses[hwnd] = f
	procSetWindowSubclass.Call(uintptr(hwnd), subclassProc, 2, 0)
}

func subclassCallback(hwnd, msg, wparam, lparam, id, ref uintptr) uintptr {
	def := func() uintptr {
		r, _, _ := procDefSubclassProc.Call(hwnd, msg, wparam, lparam)
		return r
	}
	h := HWND(hwnd)
	f := subclasses[h]
	if msg == wmNCDestroy {
		delete(subclasses, h)
		procRemoveWindowSubclass.Call(hwnd, subclassProc, id)
		return def()
	}
	if f == nil {
		return def()
	}
	return f(h, uint32(msg), wparam, lparam, def)
}

// ButtonStateChange reports whether msg can change how a button looks:
// focus, press, check, enabled state, text or the keyboard cues.
func ButtonStateChange(msg uint32) bool {
	switch msg {
	case wmSetFocus, wmKillFocus, BM_SETSTATE, BM_SETCHECK, BM_SETSTYLE, wmEnable, wmSetText,
		WM_UPDATEUISTATE, wmKeyDown, wmKeyUp, wmLButtonDown, wmLButtonUp, wmLButtonDblClk,
		wmCaptureChanged:
		return true
	}
	return false
}
