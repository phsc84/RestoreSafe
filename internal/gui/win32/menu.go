package win32

//lint:file-ignore ST1003 names follow the Windows SDK headers, so they match the Win32 documentation

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
	procRegisterClipFmt  = user32.NewProc("RegisterClipboardFormatW")
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
	return copyUTF16(owner, u, false)
}

// CopySecret puts a secret, given as UTF-8 bytes, on the clipboard, marked
// so that Windows keeps it out of the clipboard history and the cloud
// clipboard, and clipboard monitors leave it alone. It zeroes its UTF-16
// copy afterwards; the clipboard keeps its own.
func CopySecret(owner HWND, secret []byte) error {
	u := secretUTF16(secret)
	defer clear(u)
	return copyUTF16(owner, u, true)
}

// Clipboard formats that mark a secret; their value is a DWORD 0.
var secretClipboardFormats = []string{"ExcludeClipboardContentFromMonitorProcessing", "CanIncludeInClipboardHistory", "CanUploadToCloudClipboard"}

// copyUTF16 puts the NUL-terminated text u on the clipboard.
func copyUTF16(owner HWND, u []uint16, secret bool) error {
	if r, _, err := procOpenClipboard.Call(uintptr(owner)); r == 0 {
		return lastErr("OpenClipboard", err)
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if err := setClipboardData(cfUnicodeText, unsafe.Pointer(&u[0]), uintptr(len(u)*2)); err != nil {
		return err
	}
	if !secret {
		return nil
	}
	var zero uint32
	for _, name := range secretClipboardFormats {
		n, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return err
		}
		format, _, err := procRegisterClipFmt.Call(uintptr(unsafe.Pointer(n)))
		if format == 0 {
			return lastErr("RegisterClipboardFormat", err)
		}
		if err := setClipboardData(format, unsafe.Pointer(&zero), unsafe.Sizeof(zero)); err != nil {
			return err
		}
	}
	return nil
}

// setClipboardData copies size bytes at p to the open clipboard as format.
func setClipboardData(format uintptr, p unsafe.Pointer, size uintptr) error {
	h, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return lastErr("GlobalAlloc", err)
	}
	dst, _, _ := procGlobalLock.Call(h)
	if dst == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock failed")
	}
	procRtlMoveMemory.Call(dst, uintptr(p), size)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
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

var procGetFocus = user32.NewProc("GetFocus")

// Focus returns the window with the keyboard focus on this thread.
func Focus() HWND {
	r, _, _ := procGetFocus.Call()
	return HWND(r)
}

// SetHandCursor shows the hand cursor of links.
func SetHandCursor() {
	c, _, _ := procLoadCursorW.Call(0, idcHand)
	procSetCursor.Call(c)
}

const idcHand = 32649

var procGetWindow = user32.NewProc("GetWindow")

const (
	gwChild    = 5
	gwHwndNext = 2
)

// ChildWindows returns the direct child windows of parent, in z-order.
func ChildWindows(parent HWND) []HWND {
	var out []HWND
	h, _, _ := procGetWindow.Call(uintptr(parent), gwChild)
	for h != 0 {
		out = append(out, HWND(h))
		h, _, _ = procGetWindow.Call(h, gwHwndNext)
	}
	return out
}

// ChildRect returns the rectangle of child in its parent's client
// coordinates.
func ChildRect(child HWND) Rect {
	r := WindowRect(child)
	tl := ScreenToClient(Parent(child), Point{X: r.Left, Y: r.Top})
	return Rect{Left: tl.X, Top: tl.Y, Right: tl.X + r.Width(), Bottom: tl.Y + r.Height()}
}
