// Package win32 is a thin wrapper over the Win32 functions, structures, and
// constants the RestoreSafe GUI uses. It contains no logic: each function
// calls one Windows API and converts its failure into an error.
package win32

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HWND is a window handle.
type HWND = windows.HWND

// Point is a POINT.
type Point struct{ X, Y int32 }

// Rect is a RECT.
type Rect struct{ Left, Top, Right, Bottom int32 }

// Width returns the width of r.
func (r Rect) Width() int32 { return r.Right - r.Left }

// Height returns the height of r.
func (r Rect) Height() int32 { return r.Bottom - r.Top }

// Msg is a MSG.
type Msg struct {
	Hwnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
	Private uint32
}

// WndClassEx is a WNDCLASSEXW.
type WndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

// MinMaxInfo is a MINMAXINFO.
type MinMaxInfo struct {
	Reserved     Point
	MaxSize      Point
	MaxPosition  Point
	MinTrackSize Point
	MaxTrackSize Point
}

type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

// SetTextEx is a SETTEXTEX (rich edit).
type SetTextEx struct {
	Flags    uint32
	Codepage uint32
}

// Window styles and messages.
const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_TABSTOP          = 0x00010000
	WS_VSCROLL          = 0x00200000
	WS_BORDER           = 0x00800000
	WS_CLIPCHILDREN     = 0x02000000
	WS_EX_CONTROLPARENT = 0x00010000

	BS_PUSHBUTTON    = 0x0
	BS_DEFPUSHBUTTON = 0x1

	SS_LEFT         = 0x0
	SS_CENTERIMAGE  = 0x200
	SS_PATHELLIPSIS = 0x8000
	SS_NOPREFIX     = 0x80

	ES_MULTILINE   = 0x4
	ES_READONLY    = 0x800
	ES_AUTOVSCROLL = 0x40

	CW_USEDEFAULT = ^0x7fffffff

	SW_SHOWNORMAL = 1

	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_VSCROLL        = 0x0115
	SB_BOTTOM         = 7
	WM_CLOSE          = 0x0010
	WM_GETMINMAXINFO  = 0x0024
	WM_SETFONT        = 0x0030
	WM_SETICON        = 0x0080
	WM_COMMAND        = 0x0111
	WM_CTLCOLORSTATIC = 0x0138
	WM_DPICHANGED     = 0x02E0
	WM_APP            = 0x8000

	BN_CLICKED = 0

	ICON_SMALL = 0
	ICON_BIG   = 1

	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010

	COLOR_WINDOW = 5

	IDC_ARROW = 32512

	IMAGE_ICON   = 1
	LR_SHARED    = 0x8000
	SM_CXICON    = 11
	SM_CYICON    = 12
	SM_CXSMICON  = 49
	SM_CXVSCROLL = 2
	SM_CXBORDER  = 5
	SM_CYSMICON  = 50
	// SM_CYFULLSCREEN is the client height of a full-screen window.
	SM_CYFULLSCREEN = 17
	// WM_SIZING is sent while the user drags a window's frame.
	WM_SIZING    = 0x0214
	MB_OK        = 0x0
	MB_ICONERROR = 0x10

	SPI_GETNONCLIENTMETRICS = 0x0029

	MONITOR_DEFAULTTONEAREST = 2
	MDT_EFFECTIVE_DPI        = 0

	ICC_STANDARD_CLASSES = 0x4000
	ICC_PROGRESS_CLASS   = 0x20
	ICC_LISTVIEW_CLASSES = 0x01

	FW_BOLD = 700

	TRANSPARENT = 1

	// Rich edit.
	MSFTEDIT_CLASS   = "RICHEDIT50W"
	EM_SETBKGNDCOLOR = 0x0443
	EM_SETTEXTEX     = 0x0461
	EM_SETZOOM       = 0x04E1
	EM_SETMARGINS    = 0x00D3
	EC_LEFTMARGIN    = 0x1
	EC_RIGHTMARGIN   = 0x2
	ST_DEFAULT       = 0
	CP_UTF8          = 65001
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	comctl32 = windows.NewLazySystemDLL("comctl32.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procUpdateWindow             = user32.NewProc("UpdateWindow")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procIsDialogMessageW         = user32.NewProc("IsDialogMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procSetWindowTextW           = user32.NewProc("SetWindowTextW")
	procEnableWindow             = user32.NewProc("EnableWindow")
	procSetFocus                 = user32.NewProc("SetFocus")
	procLoadCursorW              = user32.NewProc("LoadCursorW")
	procLoadImageW               = user32.NewProc("LoadImageW")
	procGetSysColorBrush         = user32.NewProc("GetSysColorBrush")
	procGetSysColor              = user32.NewProc("GetSysColor")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procGetSystemMetricsForDpi   = user32.NewProc("GetSystemMetricsForDpi")
	procSystemParametersForDpi   = user32.NewProc("SystemParametersInfoForDpi")
	procAdjustWindowRectExForDpi = user32.NewProc("AdjustWindowRectExForDpi")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procMonitorFromPoint         = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procGetDpiForMonitor         = shcore.NewProc("GetDpiForMonitor")
	procCreateFontIndirectW      = gdi32.NewProc("CreateFontIndirectW")
	procDeleteObject             = gdi32.NewProc("DeleteObject")
	procSetBkMode                = gdi32.NewProc("SetBkMode")
	procInitCommonControlsEx     = comctl32.NewProc("InitCommonControlsEx")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

// lastErr turns the error of a failed call into a descriptive error.
func lastErr(fn string, err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return fmt.Errorf("%s failed", fn)
	}
	return fmt.Errorf("%s failed: %w", fn, err)
}

// UTF16 converts s for a Windows call; s must not contain NUL.
func UTF16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("")
	}
	return p
}

// ModuleHandle returns the handle of the executable.
func ModuleHandle() windows.Handle {
	h, _, _ := procGetModuleHandleW.Call(0)
	return windows.Handle(h)
}

// InitCommonControls registers the common control classes the GUI uses.
func InitCommonControls() error {
	icc := initCommonControlsEx{ICC: ICC_STANDARD_CLASSES | ICC_PROGRESS_CLASS | ICC_LINK_CLASS | ICC_LISTVIEW_CLASSES | ICC_BAR_CLASSES}
	icc.Size = uint32(unsafe.Sizeof(icc))
	if r, _, err := procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc))); r == 0 {
		return lastErr("InitCommonControlsEx", err)
	}
	return nil
}

// LoadRichEdit loads the rich edit control (MSFTEDIT_CLASS).
func LoadRichEdit() error {
	if _, err := windows.LoadLibrary("Msftedit.dll"); err != nil {
		return fmt.Errorf("Loading Msftedit.dll failed: %w", err)
	}
	return nil
}

// RegisterClass registers a window class.
func RegisterClass(wc *WndClassEx) error {
	wc.Size = uint32(unsafe.Sizeof(*wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc))); r == 0 {
		return lastErr("RegisterClassExW", err)
	}
	return nil
}

// CreateWindow creates a window or control.
func CreateWindow(exStyle uint32, class, title string, style uint32, x, y, w, h int32, parent HWND, id uintptr) (HWND, error) {
	r, _, err := procCreateWindowExW.Call(
		uintptr(exStyle), uintptr(unsafe.Pointer(UTF16(class))), uintptr(unsafe.Pointer(UTF16(title))),
		uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(parent), id, uintptr(ModuleHandle()), 0)
	if r == 0 {
		return 0, lastErr("CreateWindowExW("+class+")", err)
	}
	return HWND(r), nil
}

// DefWindowProc calls the default window procedure.
func DefWindowProc(hwnd HWND, msg uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)
	return r
}

// DestroyWindow destroys a window.
func DestroyWindow(hwnd HWND) { procDestroyWindow.Call(uintptr(hwnd)) }

// ShowWindow shows a window.
func ShowWindow(hwnd HWND, cmd int32) {
	procShowWindow.Call(uintptr(hwnd), uintptr(cmd))
	procUpdateWindow.Call(uintptr(hwnd))
}

// GetMessage retrieves the next message; it returns false for WM_QUIT.
func GetMessage(msg *Msg) (bool, error) {
	r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0)
	switch int32(r) {
	case -1:
		return false, lastErr("GetMessageW", err)
	case 0:
		return false, nil
	}
	return true, nil
}

// IsDialogMessage handles keyboard navigation (Tab, Enter, Esc) for hwnd.
func IsDialogMessage(hwnd HWND, msg *Msg) bool {
	r, _, _ := procIsDialogMessageW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(msg)))
	return r != 0
}

// TranslateAndDispatch translates and dispatches msg.
func TranslateAndDispatch(msg *Msg) {
	procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
	procDispatchMessageW.Call(uintptr(unsafe.Pointer(msg)))
}

// PostQuitMessage ends the message loop.
func PostQuitMessage(code int32) { procPostQuitMessage.Call(uintptr(code)) }

// PostMessage posts a message; it is safe to call from any goroutine.
func PostMessage(hwnd HWND, msg uint32, wparam, lparam uintptr) error {
	if r, _, err := procPostMessageW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam); r == 0 {
		return lastErr("PostMessageW", err)
	}
	return nil
}

// SendMessage sends a message and returns its result.
func SendMessage(hwnd HWND, msg uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)
	return r
}

// SetWindowPos moves and sizes a window without changing its Z order.
func SetWindowPos(hwnd HWND, r Rect) {
	procSetWindowPos.Call(uintptr(hwnd), 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Width()), uintptr(r.Height()), SWP_NOZORDER|SWP_NOACTIVATE)
}

// ClientRect returns the client area of hwnd.
func ClientRect(hwnd HWND) Rect {
	var r Rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

// SetText sets the text of a window or control.
func SetText(hwnd HWND, text string) {
	procSetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(UTF16(text))))
}

// Enable enables or disables a window.
func Enable(hwnd HWND, enabled bool) {
	var b uintptr
	if enabled {
		b = 1
	}
	procEnableWindow.Call(uintptr(hwnd), b)
}

// SetFocus gives hwnd the keyboard focus.
func SetFocus(hwnd HWND) { procSetFocus.Call(uintptr(hwnd)) }

// ArrowCursor returns the standard arrow cursor.
func ArrowCursor() windows.Handle {
	r, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	return windows.Handle(r)
}

// LoadIcon loads icon resource id of the executable in the given size; 0 when
// it is missing.
func LoadIcon(id uintptr, size int32) windows.Handle {
	r, _, _ := procLoadImageW.Call(uintptr(ModuleHandle()), id, IMAGE_ICON, uintptr(size), uintptr(size), LR_SHARED)
	return windows.Handle(r)
}

// SysColorBrush returns the system brush for a COLOR_* index.
func SysColorBrush(index int32) windows.Handle {
	r, _, _ := procGetSysColorBrush.Call(uintptr(index))
	return windows.Handle(r)
}

// SysColor returns the COLORREF of a COLOR_* index.
func SysColor(index int32) uint32 {
	r, _, _ := procGetSysColor.Call(uintptr(index))
	return uint32(r)
}

// SetBkModeTransparent makes text drawn on hdc keep the background.
func SetBkModeTransparent(hdc uintptr) { procSetBkMode.Call(hdc, TRANSPARENT) }

// MessageBox shows a message box owned by hwnd (may be 0).
func MessageBox(hwnd HWND, text, caption string, flags uint32) {
	windows.MessageBox(hwnd, UTF16(text), UTF16(caption), flags) //nolint:errcheck
}

// ShellOpen opens a file or folder with its default application.
func ShellOpen(hwnd HWND, path string) error {
	return windows.ShellExecute(windows.Handle(hwnd), UTF16("open"), UTF16(path), nil, nil, SW_SHOWNORMAL)
}

// SetRichText sets the content of a rich edit control from RTF (UTF-8).
func SetRichText(hwnd HWND, rtf string) {
	st := SetTextEx{Flags: ST_DEFAULT, Codepage: CP_UTF8}
	b := append([]byte(rtf), 0)
	SendMessage(hwnd, EM_SETTEXTEX, uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(&b[0])))
}

// LoWord returns the low 16 bits of v.
func LoWord(v uintptr) uint16 { return uint16(v) }

// HiWord returns bits 16-31 of v.
func HiWord(v uintptr) uint16 { return uint16(v >> 16) }

// MinMaxInfoParam returns the MINMAXINFO of a WM_GETMINMAXINFO lparam.
func MinMaxInfoParam(lparam uintptr) *MinMaxInfo {
	return *(**MinMaxInfo)(unsafe.Pointer(&lparam))
}

// RectParam returns the RECT of a WM_DPICHANGED lparam.
func RectParam(lparam uintptr) *Rect {
	return *(**Rect)(unsafe.Pointer(&lparam))
}

// ShellOpenWith starts exe with args (e.g. notepad.exe with a file).
func ShellOpenWith(hwnd HWND, exe, args string) error {
	return windows.ShellExecute(windows.Handle(hwnd), UTF16("open"), UTF16(exe), UTF16(args), nil, SW_SHOWNORMAL)
}
