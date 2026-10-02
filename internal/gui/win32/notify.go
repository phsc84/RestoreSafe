package win32

import (
	"unsafe"
)

// Notification, checkbox, edit and default-button constants.
const (
	WM_NOTIFY = 0x004E
	NM_DBLCLK = ^uint32(2) // -3
	NM_RETURN = ^uint32(3) // -4

	BS_AUTOCHECKBOX = 0x0003
	BM_GETCHECK     = 0x00F0
	BM_SETCHECK     = 0x00F1
	BST_CHECKED     = 1
	EN_CHANGE       = 0x0300

	DM_GETDEFID = 0x0400
	DC_HASDEFID = 0x534B
)

// NMHdr is an NMHDR, the header of every WM_NOTIFY message.
type NMHdr struct {
	HwndFrom HWND
	IDFrom   uintptr
	Code     uint32
}

// NMHdrParam returns the NMHDR of a WM_NOTIFY lparam.
func NMHdrParam(lparam uintptr) *NMHdr {
	return *(**NMHdr)(unsafe.Pointer(&lparam))
}

// Checked reports whether a checkbox is checked.
func Checked(hwnd HWND) bool { return SendMessage(hwnd, BM_GETCHECK, 0, 0) == BST_CHECKED }

// SetChecked checks or unchecks a checkbox.
func SetChecked(hwnd HWND, checked bool) {
	var v uintptr
	if checked {
		v = BST_CHECKED
	}
	SendMessage(hwnd, BM_SETCHECK, v, 0)
}
