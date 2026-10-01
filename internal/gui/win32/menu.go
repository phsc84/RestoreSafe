package win32

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPosM    = user32.NewProc("GetCursorPos")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procSetCapture       = user32.NewProc("SetCapture")
	procReleaseCapture   = user32.NewProc("ReleaseCapture")
	procSetCursor        = user32.NewProc("SetCursor")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	kernel32Global       = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalAlloc      = kernel32Global.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32Global.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32Global.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32Global.NewProc("GlobalFree")
	procRtlMoveMemory    = kernel32Global.NewProc("RtlMoveMemory")
)

const (
	mfString      = 0x0000
	mfGrayed      = 0x0001
	mfSeparator   = 0x0800
	tpmReturnCmd  = 0x0100
	tpmRightBtn   = 0x0002
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	// Combo box (drop-down list).
	CBS_DROPDOWNLIST = 0x0003
	CB_ADDSTRING     = 0x0143
	CB_GETCURSEL     = 0x0147
	CB_RESETCONTENT  = 0x014B
	CB_SETCURSEL     = 0x014E
	CBN_SELCHANGE    = 1

	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONUP   = 0x0202
	WM_SETCURSOR   = 0x0020
	WM_CONTEXTMENU = 0x007B
	IDC_SIZENS     = 32645
)

// MenuItem is an entry of a context menu; Text "" is a separator.
type MenuItem struct {
	ID       uint16
	Text     string
	Disabled bool
}

// ShowMenu shows a context menu at pt (screen coordinates) and returns the
// ID of the chosen item, 0 when none.
func ShowMenu(owner HWND, pt Point, items []MenuItem) uint16 {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return 0
	}
	defer procDestroyMenu.Call(menu)
	for _, it := range items {
		if it.Text == "" {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Disabled {
			flags |= mfGrayed
		}
		procAppendMenuW.Call(menu, flags, uintptr(it.ID), uintptr(unsafe.Pointer(UTF16(it.Text))))
	}
	r, _, _ := procTrackPopupMenuEx.Call(menu, tpmReturnCmd|tpmRightBtn, uintptr(pt.X), uintptr(pt.Y), uintptr(owner), 0)
	return uint16(r)
}

// CursorPos returns the cursor position in screen coordinates.
func CursorPos() Point {
	var p Point
	procGetCursorPosM.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

// ScreenToClient converts pt to client coordinates of hwnd.
func ScreenToClient(hwnd HWND, pt Point) Point {
	procScreenToClient.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	return pt
}

// ClientToScreen converts pt from client coordinates of hwnd.
func ClientToScreen(hwnd HWND, pt Point) Point {
	procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	return pt
}

// CopyText puts text on the clipboard.
func CopyText(owner HWND, text string) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	if r, _, err := procOpenClipboard.Call(uintptr(owner)); r == 0 {
		return lastErr("OpenClipboard", err)
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	size := uintptr(len(u) * 2)
	h, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return lastErr("GlobalAlloc", err)
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock failed")
	}
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&u[0])), size)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return lastErr("SetClipboardData", err)
	}
	return nil
}

// SetCapture sends the mouse input to hwnd until ReleaseCapture.
func SetCapture(hwnd HWND) { procSetCapture.Call(uintptr(hwnd)) }

// ReleaseCapture ends SetCapture.
func ReleaseCapture() { procReleaseCapture.Call() }

// SetSizeNSCursor shows the north-south resize cursor.
func SetSizeNSCursor() {
	c, _, _ := procLoadCursorW.Call(0, IDC_SIZENS)
	procSetCursor.Call(c)
}

// ComboSet replaces the entries of a drop-down list and selects index.
func ComboSet(cb HWND, entries []string, index int) {
	SendMessage(cb, CB_RESETCONTENT, 0, 0)
	for _, e := range entries {
		SendMessage(cb, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(UTF16(e))))
	}
	SendMessage(cb, CB_SETCURSEL, uintptr(index), 0)
}

// ComboSelected returns the selected entry of a drop-down list, or -1.
func ComboSelected(cb HWND) int { return int(int32(SendMessage(cb, CB_GETCURSEL, 0, 0))) }

// PointParam returns the point packed in an lparam (WM_MOUSEMOVE, ...).
func PointParam(lparam uintptr) Point {
	return Point{X: int32(int16(lparam & 0xFFFF)), Y: int32(int16(lparam >> 16 & 0xFFFF))}
}
