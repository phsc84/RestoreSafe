package win32

import (
	"unsafe"
)

// Tree view, checkbox, notification, and default-button constants.
const (
	WC_TREEVIEW = "SysTreeView32"

	TVS_HASBUTTONS    = 0x0001
	TVS_HASLINES      = 0x0002
	TVS_LINESATROOT   = 0x0004
	TVS_SHOWSELALWAYS = 0x0020

	tvmInsertItemW   = 0x1132
	tvmDeleteItem    = 0x1101
	tvmExpand        = 0x1102
	tvmSelectItem    = 0x110B
	tvmGetNextItem   = 0x110A
	tvifText         = 0x0001
	tvifParam        = 0x0004
	tveExpand        = 0x0002
	tvgnCaret        = 0x0009
	tvgnFirstVisible = 0x0005

	WM_NOTIFY       = 0x004E
	TVN_SELCHANGEDW = ^uint32(450) // TVN_FIRST-51 = -451
	NM_DBLCLK       = ^uint32(2)   // -3
	NM_RETURN       = ^uint32(3)   // -4

	BS_AUTOCHECKBOX = 0x0003
	BM_GETCHECK     = 0x00F0
	BM_SETCHECK     = 0x00F1
	BST_CHECKED     = 1
	EN_CHANGE       = 0x0300

	DM_GETDEFID = 0x0400
	DC_HASDEFID = 0x534B
)

// tvItem is a TVITEMW.
type tvItem struct {
	Mask          uint32
	HItem         uintptr
	State         uint32
	StateMask     uint32
	Text          *uint16
	TextMax       int32
	Image         int32
	SelectedImage int32
	Children      int32
	LParam        uintptr
}

// tvInsertStruct is a TVINSERTSTRUCTW; the item union is as large as
// TVITEMEXW (24 more bytes than TVITEMW).
type tvInsertStruct struct {
	Parent      uintptr
	InsertAfter uintptr
	Item        tvItem
	_           [24]byte
}

// TreeItem is a tree view item handle.
type TreeItem uintptr

var (
	tviRoot = ^uintptr(0xFFFF) // TVI_ROOT = -0x10000
	tviLast = ^uintptr(0xFFFD) // TVI_LAST = -0xFFFE
)

// InsertTreeItem appends an item with text under parent (0: at the root).
func InsertTreeItem(tree HWND, parent TreeItem, text string) TreeItem {
	p := uintptr(parent)
	if p == 0 {
		p = tviRoot
	}
	is := tvInsertStruct{Parent: p, InsertAfter: tviLast, Item: tvItem{Mask: tvifText, Text: UTF16(text)}}
	return TreeItem(SendMessage(tree, tvmInsertItemW, 0, uintptr(unsafe.Pointer(&is))))
}

// ClearTree removes all items.
func ClearTree(tree HWND) { SendMessage(tree, tvmDeleteItem, 0, tviRoot) }

// ExpandTreeItem shows the children of item.
func ExpandTreeItem(tree HWND, item TreeItem) { SendMessage(tree, tvmExpand, tveExpand, uintptr(item)) }

// SelectTreeItem selects item and scrolls it into view.
func SelectTreeItem(tree HWND, item TreeItem) {
	SendMessage(tree, tvmSelectItem, tvgnCaret, uintptr(item))
	SendMessage(tree, tvmSelectItem, tvgnFirstVisible, uintptr(item))
}

// TreeSelection returns the selected item, or 0.
func TreeSelection(tree HWND) TreeItem {
	return TreeItem(SendMessage(tree, tvmGetNextItem, tvgnCaret, 0))
}

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
