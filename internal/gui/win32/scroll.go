package win32

//lint:file-ignore ST1003 names follow the Windows SDK headers, so they match the Win32 documentation

import "unsafe"

var (
	procSetScrollInfo = user32.NewProc("SetScrollInfo")
	procGetScrollInfo = user32.NewProc("GetScrollInfo")
)

// Scrolling.
const (
	WM_MOUSEWHEEL = 0x020A
	SB_VERT       = 1
	SB_LINEUP     = 0
	SB_LINEDOWN   = 1
	SB_PAGEUP     = 2
	SB_PAGEDOWN   = 3
	SB_THUMBTRACK = 5
	SB_TOP        = 6
	sifRange      = 0x1
	sifPage       = 0x2
	sifPos        = 0x4
	sifTrackPos   = 0x10
	WHEEL_DELTA   = 120
)

// scrollInfo is SCROLLINFO.
type scrollInfo struct {
	Size     uint32
	Mask     uint32
	Min, Max int32
	Page     uint32
	Pos      int32
	TrackPos int32
}

// SetVScroll sets the vertical scroll bar of hwnd for content of height
// total shown in page pixels at pos; the bar hides when everything fits.
func SetVScroll(hwnd HWND, total, page, pos int32) {
	si := scrollInfo{Mask: sifRange | sifPage | sifPos, Max: max(total-1, 0), Page: uint32(max(page, 0)), Pos: pos}
	si.Size = uint32(unsafe.Sizeof(si))
	procSetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&si)), 1)
}

// VScrollTrack returns the position of the vertical scroll box while it is
// dragged.
func VScrollTrack(hwnd HWND) int32 {
	si := scrollInfo{Mask: sifTrackPos}
	si.Size = uint32(unsafe.Sizeof(si))
	procGetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&si)))
	return si.TrackPos
}
