package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Tooltips (tooltips_class32).
const (
	TOOLTIPS_CLASS    = "tooltips_class32"
	ICC_BAR_CLASSES   = 0x4
	ttsAlwaysTip      = 0x01
	ttsNoPrefix       = 0x02
	ttfIDIsHwnd       = 0x0001
	ttfSubclass       = 0x0010
	ttmAddToolW       = 0x0432
	ttmDelToolW       = 0x0433
	ttmSetMaxTipWidth = 0x0418
	wsExTopmost       = 0x00000008
)

// toolInfo is TOOLINFOW.
type toolInfo struct {
	Size     uint32
	Flags    uint32
	Hwnd     HWND
	ID       uintptr
	Rect     Rect
	Instance windows.Handle
	Text     *uint16
	Param    uintptr
	Reserved uintptr
}

// NewTooltipWindow creates a tooltip window owned by owner; tips wrap at
// maxWidth pixels.
func NewTooltipWindow(owner HWND, maxWidth int32) (HWND, error) {
	h, err := CreateWindow(wsExTopmost, TOOLTIPS_CLASS, "", WS_POPUP|ttsAlwaysTip|ttsNoPrefix, 0, 0, 0, 0, owner, 0)
	if err != nil {
		return 0, err
	}
	SendMessage(h, ttmSetMaxTipWidth, 0, uintptr(maxWidth))
	return h, nil
}

// AddTooltip shows text when the mouse rests on control. A static control
// gets SS_NOTIFY, so it receives the mouse.
func AddTooltip(tip, control HWND, text string) {
	if Class(control) == "Static" {
		SetStyle(control, Style(control)|SS_NOTIFY)
	}
	t, _ := windows.UTF16PtrFromString(text)
	ti := toolInfo{Flags: ttfIDIsHwnd | ttfSubclass, Hwnd: Parent(control), ID: uintptr(control), Text: t}
	ti.Size = uint32(unsafe.Sizeof(ti))
	SendMessage(tip, ttmAddToolW, 0, uintptr(unsafe.Pointer(&ti)))
}

// RemoveTooltip removes the tip of control.
func RemoveTooltip(tip, control HWND) {
	ti := toolInfo{Hwnd: Parent(control), ID: uintptr(control)}
	ti.Size = uint32(unsafe.Sizeof(ti))
	SendMessage(tip, ttmDelToolW, 0, uintptr(unsafe.Pointer(&ti)))
}

var procGetClassNameW = user32.NewProc("GetClassNameW")

// Class returns the window class name of hwnd.
func Class(hwnd HWND) string {
	buf := make([]uint16, 64)
	n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// TTN_GETDISPINFOW asks a callback tool's parent for the tip's text each
// time the tip is about to show.
const TTN_GETDISPINFOW = ^uint32(530 - 1) // -530

// NMTTDispInfo is NMTTDISPINFOW.
type NMTTDispInfo struct {
	Hdr      NMHdr
	Text     *uint16
	Buf      [80]uint16
	Instance windows.Handle
	Flags    uint32
	Param    uintptr
}

// TooltipTextOf returns the NMTTDISPINFOW behind hdr.
func TooltipTextOf(hdr *NMHdr) *NMTTDispInfo { return (*NMTTDispInfo)(unsafe.Pointer(hdr)) }

// AddTooltipCallback adds control to tip; its text comes from
// TTN_GETDISPINFOW to the control's parent, and an empty text shows
// nothing. A static control gets SS_NOTIFY, so it receives the mouse.
func AddTooltipCallback(tip, control HWND) {
	if Class(control) == "Static" {
		SetStyle(control, Style(control)|SS_NOTIFY)
	}
	ti := toolInfo{Flags: ttfIDIsHwnd | ttfSubclass, Hwnd: Parent(control), ID: uintptr(control)}
	ti.Size = uint32(unsafe.Sizeof(ti))
	// LPSTR_TEXTCALLBACKW is the pointer value -1, which Go can't hold in
	// a *uint16; the struct is copied with the field patched.
	raw := *(*[unsafe.Sizeof(ti)]byte)(unsafe.Pointer(&ti))
	*(*uintptr)(unsafe.Pointer(&raw[unsafe.Offsetof(ti.Text)])) = ^uintptr(0)
	SendMessage(tip, ttmAddToolW, 0, uintptr(unsafe.Pointer(&raw)))
}
