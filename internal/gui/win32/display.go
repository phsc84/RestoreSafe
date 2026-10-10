package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// LogFont is a LOGFONTW.
type LogFont struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

// NonClientMetrics is a NONCLIENTMETRICSW.
type NonClientMetrics struct {
	Size              uint32
	BorderWidth       int32
	ScrollWidth       int32
	ScrollHeight      int32
	CaptionWidth      int32
	CaptionHeight     int32
	CaptionFont       LogFont
	SmCaptionWidth    int32
	SmCaptionHeight   int32
	SmCaptionFont     LogFont
	MenuWidth         int32
	MenuHeight        int32
	MenuFont          LogFont
	StatusFont        LogFont
	MessageFont       LogFont
	PaddedBorderWidth int32
}

// MonitorInfo is a MONITORINFO.
type MonitorInfo struct {
	Size    uint32
	Monitor Rect
	Work    Rect
	Flags   uint32
}

// DpiForWindow returns the DPI of the monitor hwnd is on.
func DpiForWindow(hwnd HWND) uint32 {
	r, _, _ := procGetDpiForWindow.Call(uintptr(hwnd))
	if r == 0 {
		return 96
	}
	return uint32(r)
}

// DpiForSystem returns the system DPI (the DPI of the primary monitor at
// logon).
func DpiForSystem() uint32 {
	r, _, _ := procGetDpiForSystem.Call()
	if r == 0 {
		return 96
	}
	return uint32(r)
}

// SystemMetric returns a SM_* metric for dpi.
func SystemMetric(index int32, dpi uint32) int32 {
	r, _, _ := procGetSystemMetricsForDpi.Call(uintptr(index), uintptr(dpi))
	return int32(r)
}

// MessageFont returns the system message font for dpi.
func MessageFont(dpi uint32) (LogFont, error) {
	var ncm NonClientMetrics
	ncm.Size = uint32(unsafe.Sizeof(ncm))
	if r, _, err := procSystemParametersForDpi.Call(SPI_GETNONCLIENTMETRICS, uintptr(ncm.Size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(dpi)); r == 0 {
		return LogFont{}, lastErr("SystemParametersInfoForDpi", err)
	}
	return ncm.MessageFont, nil
}

// WindowRectForClient returns the window size needed for a client area of
// the given size at dpi.
func WindowRectForClient(client Rect, style, exStyle uint32, dpi uint32) Rect {
	r := client
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, uintptr(exStyle), uintptr(dpi))
	return r
}

// CursorMonitor returns the work area and effective DPI of the monitor the
// mouse cursor is on.
func CursorMonitor() (work Rect, dpi uint32) {
	var pt Point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	mon, _, _ := procMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, MONITOR_DEFAULTTONEAREST)
	info := MonitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&info)))
	var x, y uint32
	if r, _, _ := procGetDpiForMonitor.Call(mon, MDT_EFFECTIVE_DPI, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r != 0 || y == 0 {
		y = 96
	}
	return info.Work, y
}

// CreateFont creates a font; the caller deletes it with DeleteObject.
func CreateFont(lf *LogFont) (windows.Handle, error) {
	r, _, err := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(lf)))
	if r == 0 {
		return 0, lastErr("CreateFontIndirectW", err)
	}
	return windows.Handle(r), nil
}

// DeleteObject deletes a GDI object; 0 is ignored.
func DeleteObject(h windows.Handle) {
	if h != 0 {
		procDeleteObject.Call(uintptr(h))
	}
}

// SetFont sets the font of a control and redraws it.
func SetFont(hwnd HWND, font windows.Handle) {
	SendMessage(hwnd, WM_SETFONT, uintptr(font), 1)
}

// Face returns the face name of lf.
func (lf *LogFont) Face() string { return windows.UTF16ToString(lf.FaceName[:]) }
