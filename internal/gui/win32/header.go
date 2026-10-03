package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The column header (SysHeader32) of a list view in report mode.
const (
	hdmFirst        = 0x1200
	hdmGetItemCount = hdmFirst + 0
	hdmGetItemRect  = hdmFirst + 7
	hdmGetItemW     = hdmFirst + 11
	hdiText         = 0x0002
	hdiFormat       = 0x0004
	hdfRight        = 0x0001
)

// hdItem is HDITEMW.
type hdItem struct {
	Mask    uint32
	Cxy     int32
	Text    *uint16
	Bitmap  uintptr
	TextMax int32
	Fmt     int32
	Param   uintptr
	Image   int32
	Order   int32
	Type    uint32
	Filter  uintptr
	State   uint32
}

// ListHeader returns the column header of the list view lv.
func ListHeader(lv HWND) HWND { return HWND(SendMessage(lv, LVM_GETHEADER, 0, 0)) }

// HeaderCount returns the number of columns of header h.
func HeaderCount(h HWND) int { return int(SendMessage(h, hdmGetItemCount, 0, 0)) }

// HeaderItemRect returns the rectangle of column i of header h.
func HeaderItemRect(h HWND, i int) Rect {
	var r Rect
	SendMessage(h, hdmGetItemRect, uintptr(i), uintptr(unsafe.Pointer(&r)))
	return r
}

// HeaderItem returns the text of column i of header h and whether it is
// right-aligned.
func HeaderItem(h HWND, i int) (string, bool) {
	buf := make([]uint16, 256)
	it := hdItem{Mask: hdiText | hdiFormat, Text: &buf[0], TextMax: int32(len(buf))}
	SendMessage(h, hdmGetItemW, uintptr(i), uintptr(unsafe.Pointer(&it)))
	return windows.UTF16ToString(buf), it.Fmt&hdfRight != 0
}
