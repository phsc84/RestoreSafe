package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Messages, flags and constants for painting, owner-drawn controls, links
// and keyboard handling.
const (
	WM_SETFOCUS      = 0x0007
	WM_KILLFOCUS     = 0x0008
	WM_PAINT         = 0x000F
	WM_ERASEBKGND    = 0x0014
	WM_GETDLGCODE    = 0x0087
	WM_KEYDOWN       = 0x0100
	WM_LBUTTONDOWN   = 0x0201
	WM_CTLCOLORBTN   = 0x0135
	WM_PRINTCLIENT   = 0x0318
	WM_UPDATEUISTATE = 0x0128

	DLGC_WANTARROWS = 0x0001
	VK_HOME         = 0x24
	VK_END          = 0x23
	VK_UP           = 0x26
	VK_DOWN         = 0x28

	BS_OWNERDRAW = 0x0B
	ODS_SELECTED = 0x0001
	ODS_DISABLED = 0x0004
	ODS_FOCUS    = 0x0010
	ODS_NOACCEL  = 0x0100

	DT_LEFT         = 0x0000
	DT_CENTER       = 0x0001
	DT_RIGHT        = 0x0002
	DT_VCENTER      = 0x0004
	DT_SINGLELINE   = 0x0020
	DT_WORDBREAK    = 0x0010
	DT_NOPREFIX     = 0x0800
	DT_CALCRECT     = 0x0400
	DT_END_ELLIPSIS = 0x8000
	DT_HIDEPREFIX   = 0x00100000

	PS_SOLID = 0
	SRCCOPY  = 0x00CC0020

	// SysLink.
	WC_LINK         = "SysLink"
	ICC_LINK_CLASS  = 0x8000
	LWS_TRANSPARENT = 0x0001
	NM_CLICK        = ^uint32(1) // -2
)

// PaintStruct is a PAINTSTRUCT.
type PaintStruct struct {
	HDC       uintptr
	Erase     int32
	Paint     Rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

var (
	procBeginPaint             = user32.NewProc("BeginPaint")
	procEndPaint               = user32.NewProc("EndPaint")
	procInvalidateRect         = user32.NewProc("InvalidateRect")
	procRedrawWindow           = user32.NewProc("RedrawWindow")
	procFillRect               = user32.NewProc("FillRect")
	procDrawTextW              = user32.NewProc("DrawTextW")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetParent              = user32.NewProc("GetParent")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procRoundRect              = gdi32.NewProc("RoundRect")
	procGetPixel               = gdi32.NewProc("GetPixel")
	procGetTextFaceW           = gdi32.NewProc("GetTextFaceW")
)

// BeginPaint starts painting hwnd; EndPaint must follow.
func BeginPaint(hwnd HWND, ps *PaintStruct) uintptr {
	r, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ps)))
	return r
}

// EndPaint ends painting hwnd.
func EndPaint(hwnd HWND, ps *PaintStruct) {
	procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ps)))
}

// Invalidate marks the whole client area of hwnd for repainting.
func Invalidate(hwnd HWND) { procInvalidateRect.Call(uintptr(hwnd), 0, 0) }

// GetDC returns the device context of hwnd's client area; ReleaseDC frees it.
func GetDC(hwnd HWND) uintptr {
	r, _, _ := procGetDC.Call(uintptr(hwnd))
	return r
}

// ReleaseDC frees a device context of GetDC.
func ReleaseDC(hwnd HWND, hdc uintptr) { procReleaseDC.Call(uintptr(hwnd), hdc) }

// Parent returns the parent window of hwnd.
func Parent(hwnd HWND) HWND {
	r, _, _ := procGetParent.Call(uintptr(hwnd))
	return HWND(r)
}

// CreateCompatibleDC returns a memory DC like hdc; DeleteDC frees it.
func CreateCompatibleDC(hdc uintptr) uintptr {
	r, _, _ := procCreateCompatibleDC.Call(hdc)
	return r
}

// DeleteDC frees a memory DC.
func DeleteDC(hdc uintptr) { procDeleteDC.Call(hdc) }

// CreateCompatibleBitmap returns a bitmap for hdc; DeleteObject frees it.
func CreateCompatibleBitmap(hdc uintptr, w, h int32) windows.Handle {
	r, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	return windows.Handle(r)
}

// SelectObject selects obj into hdc and returns the previous object.
func SelectObject(hdc uintptr, obj windows.Handle) windows.Handle {
	r, _, _ := procSelectObject.Call(hdc, uintptr(obj))
	return windows.Handle(r)
}

// BitBlt copies a w×h block from src (at sx, sy) to dst (at dx, dy).
func BitBlt(dst uintptr, dx, dy, w, h int32, src uintptr, sx, sy int32) {
	procBitBlt.Call(dst, uintptr(dx), uintptr(dy), uintptr(w), uintptr(h), src, uintptr(sx), uintptr(sy), SRCCOPY)
}

// CreateSolidBrush returns a brush of the COLORREF color; DeleteObject frees it.
func CreateSolidBrush(color uint32) windows.Handle {
	r, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return windows.Handle(r)
}

// CreatePen returns a solid pen; DeleteObject frees it.
func CreatePen(width int32, color uint32) windows.Handle {
	r, _, _ := procCreatePen.Call(PS_SOLID, uintptr(width), uintptr(color))
	return windows.Handle(r)
}

// FillRect fills r with brush.
func FillRect(hdc uintptr, r Rect, brush windows.Handle) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), uintptr(brush))
}

// RoundRect draws r with rounded corners of the given diameter, outlined
// with the selected pen and filled with the selected brush.
func RoundRect(hdc uintptr, r Rect, diameter int32) {
	procRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(diameter), uintptr(diameter))
}

// DrawText draws text in r with DT_ flags; with DT_CALCRECT it measures
// instead and returns the rectangle the text needs.
func DrawText(hdc uintptr, text string, r Rect, flags uint32) Rect {
	s, err := windows.UTF16FromString(text)
	if err != nil || len(s) <= 1 {
		return Rect{Left: r.Left, Top: r.Top, Right: r.Left, Bottom: r.Top}
	}
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)-1), uintptr(unsafe.Pointer(&r)), uintptr(flags))
	return r
}

// SelectFont selects font into hdc and returns the previous one.
func SelectFont(hdc uintptr, font windows.Handle) windows.Handle { return SelectObject(hdc, font) }

// Pixel returns the color of a pixel of hdc as a COLORREF.
func Pixel(hdc uintptr, x, y int32) uint32 {
	r, _, _ := procGetPixel.Call(hdc, uintptr(x), uintptr(y))
	return uint32(r)
}

// TextFace returns the face name of the font selected into hdc: what
// Windows uses after substituting a missing face.
func TextFace(hdc uintptr) string {
	var buf [64]uint16
	procGetTextFaceW.Call(hdc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	return windows.UTF16ToString(buf[:])
}

// Window messages and styles for custom controls.
const (
	WM_NCDESTROY   = 0x0082
	SS_ENDELLIPSIS = 0x4000
	SS_NOTIFY      = 0x0100
)

var (
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	procSelectClipRgn      = gdi32.NewProc("SelectClipRgn")
)

// ClipRoundRect limits drawing on hdc to r with rounded corners; ClipNone
// removes the limit.
func ClipRoundRect(hdc uintptr, r Rect, diameter int32) {
	rgn, _, _ := procCreateRoundRectRgn.Call(uintptr(r.Left), uintptr(r.Top), uintptr(r.Right+1), uintptr(r.Bottom+1), uintptr(diameter), uintptr(diameter))
	procSelectClipRgn.Call(hdc, rgn)
	DeleteObject(windows.Handle(rgn))
}

// ClipNone removes the clip region of hdc.
func ClipNone(hdc uintptr) { procSelectClipRgn.Call(hdc, 0) }

var procIsWindow = user32.NewProc("IsWindow")

// IsWindow reports whether hwnd is an existing window.
func IsWindow(hwnd HWND) bool {
	r, _, _ := procIsWindow.Call(uintptr(hwnd))
	return r != 0
}

// Messages and keys of the main window.
const (
	WM_ACTIVATEAPP = 0x001C
	VK_TAB         = 0x09
	VK_CONTROL     = 0x11
	VK_F5          = 0x74
	WS_THICKFRAME  = 0x00040000
)

var procGetKeyState = user32.NewProc("GetKeyState")

// KeyDown reports whether the virtual key vk is held down.
func KeyDown(vk int32) bool {
	r, _, _ := procGetKeyState.Call(uintptr(vk))
	return int16(r) < 0
}

var procIsWindowVisible = user32.NewProc("IsWindowVisible")

// IsWindowVisible reports whether hwnd has the visible style.
func IsWindowVisible(hwnd HWND) bool {
	r, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return r != 0
}

// RedrawAll repaints hwnd and all its descendants, erasing their
// backgrounds.
func RedrawAll(hwnd HWND) {
	const flags = 0x0001 | 0x0004 | 0x0080 | 0x0100 // INVALIDATE|ERASE|ALLCHILDREN|UPDATENOW
	procRedrawWindow.Call(uintptr(hwnd), 0, 0, flags)
}

// RedrawNow repaints hwnd at once, without erasing its background.
func RedrawNow(hwnd HWND) {
	const flags = 0x0001 | 0x0100 // INVALIDATE|UPDATENOW
	procRedrawWindow.Call(uintptr(hwnd), 0, 0, flags)
}
