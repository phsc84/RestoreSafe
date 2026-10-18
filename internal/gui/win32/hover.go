package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")
	procDefSubclassProc      = comctl32.NewProc("DefSubclassProc")
	procTrackMouseEvent      = user32.NewProc("TrackMouseEvent")

	hoverProc uintptr
	hot       = map[HWND]bool{}
)

const (
	wmMouseMove  = 0x0200
	wmMouseLeave = 0x02A3
	wmNCDestroy  = 0x0082
	tmeLeave     = 0x2
)

type trackMouseEvent struct {
	Size    uint32
	Flags   uint32
	Track   HWND
	HoverMs uint32
}

// TrackHover makes IsHot report whether the mouse is over hwnd; the control
// is repainted when that changes.
func TrackHover(hwnd HWND) {
	if hoverProc == 0 {
		hoverProc = windows.NewCallback(hoverSubclass)
	}
	procSetWindowSubclass.Call(uintptr(hwnd), hoverProc, 1, 0)
}

// IsHot reports whether the mouse is over a control set up with TrackHover.
func IsHot(hwnd HWND) bool { return hot[hwnd] }

func hoverSubclass(hwnd, msg, wparam, lparam, id, ref uintptr) uintptr {
	h := HWND(hwnd)
	switch msg {
	case wmMouseMove:
		if !hot[h] {
			hot[h] = true
			e := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: tmeLeave, Track: h}
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&e)))
			Invalidate(h)
		}
	case wmMouseLeave:
		delete(hot, h)
		Invalidate(h)
	case wmNCDestroy:
		delete(hot, h)
		procRemoveWindowSubclass.Call(hwnd, hoverProc, id)
	}
	r, _, _ := procDefSubclassProc.Call(hwnd, msg, wparam, lparam)
	return r
}

// TrackMouseLeave asks Windows to send hwnd WM_MOUSELEAVE when the mouse
// leaves it; a control that tracks the mouse over its parts calls it on
// the first WM_MOUSEMOVE.
func TrackMouseLeave(hwnd HWND) {
	e := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: tmeLeave, Track: hwnd}
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&e)))
}
