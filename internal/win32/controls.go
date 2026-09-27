package win32

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Styles and messages of dialogs, edits, progress bars, and sessions.
const (
	WS_POPUP            = 0x80000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_EX_DLGMODALFRAME = 0x00000001

	ES_PASSWORD        = 0x20
	SS_CENTER          = 0x1
	EM_EMPTYUNDOBUFFER = 0x00CD
	ES_AUTOHSCROLL     = 0x80

	PBS_MARQUEE        = 0x08
	PBM_SETPOS         = 0x0402
	PBM_SETRANGE32     = 0x0406
	PBM_SETMARQUEE     = 0x040A
	PROGRESS_CLASS     = "msctls_progress32"
	WM_SETTEXT         = 0x000C
	EM_SETSEL          = 0x00B1
	EM_REPLACESEL      = 0x00C2
	EM_SCROLLCARET     = 0x00B7
	EM_EXLIMITTEXT     = 0x0435
	EM_SETTEXTMODE     = 0x0459
	TM_PLAINTEXT       = 1
	WM_TIMER           = 0x0113
	WM_QUERYENDSESSION = 0x0011
	WM_ENDSESSION      = 0x0016

	IDOK     = 1
	IDCANCEL = 2

	SW_HIDE = 0
	SW_SHOW = 5
)

var (
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW       = user32.NewProc("GetWindowTextLengthW")
	procSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procShutdownBlockReasonCreate  = user32.NewProc("ShutdownBlockReasonCreate")
	procShutdownBlockReasonDestroy = user32.NewProc("ShutdownBlockReasonDestroy")
	procSetTextColor               = gdi32.NewProc("SetTextColor")
	procTaskDialogIndirect         = comctl32.NewProc("TaskDialogIndirect")
	procIsWindowEnabled            = user32.NewProc("IsWindowEnabled")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
)

// SetVisible shows or hides a control.
func SetVisible(hwnd HWND, visible bool) {
	cmd := uintptr(SW_HIDE)
	if visible {
		cmd = SW_SHOW
	}
	procShowWindow.Call(uintptr(hwnd), cmd)
}

// IsEnabled reports whether a window accepts input.
func IsEnabled(hwnd HWND) bool {
	r, _, _ := procIsWindowEnabled.Call(uintptr(hwnd))
	return r != 0
}

// SetForeground brings a window to the front.
func SetForeground(hwnd HWND) { procSetForegroundWindow.Call(uintptr(hwnd)) }

// Text returns the text of a window or control. Not for secrets: see
// ReadSecret.
func Text(hwnd HWND) string {
	buf := textBuffer(hwnd)
	return windows.UTF16ToString(buf)
}

func textBuffer(hwnd HWND) []uint16 {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	buf := make([]uint16, n+1)
	r, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return buf[:r]
}

// ReadSecret returns the text of an edit control as UTF-8 bytes without
// creating Go strings, zeroes the buffers it used, and overwrites the
// control's text (see OverwriteText), so neither keeps the secret. The
// caller zeroes the returned bytes.
func ReadSecret(hwnd HWND) []byte {
	buf := textBuffer(hwnd)
	// At most 3 UTF-8 bytes per UTF-16 unit: out never grows, so no copy of
	// the secret is left behind by a reallocation.
	out := make([]byte, 0, len(buf)*3)
	var enc [4]byte
	for i := 0; i < len(buf); {
		r := rune(buf[i])
		size := 1
		if utf16.IsSurrogate(r) && i+1 < len(buf) {
			r = utf16.DecodeRune(r, rune(buf[i+1]))
			size = 2
		}
		out = append(out, enc[:encodeRune(enc[:], r)]...)
		i += size
	}
	enc = [4]byte{}
	for i := range buf {
		buf[i] = 0
	}
	runtime.KeepAlive(enc)
	OverwriteText(hwnd)
	return out
}

// OverwriteText replaces the text of a control with filler of the same
// length before clearing it, so the control's own buffer does not keep the
// previous text, and empties an edit control's undo buffer.
func OverwriteText(hwnd HWND) {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	filler := make([]uint16, n+1)
	for i := uintptr(0); i < n; i++ {
		filler[i] = '*'
	}
	SendMessage(hwnd, WM_SETTEXT, 0, uintptr(unsafe.Pointer(&filler[0])))
	SendMessage(hwnd, WM_SETTEXT, 0, uintptr(unsafe.Pointer(&filler[n])))
	SendMessage(hwnd, EM_EMPTYUNDOBUFFER, 0, 0)
	runtime.KeepAlive(filler)
}

// encodeRune writes r as UTF-8 into p and returns the number of bytes.
func encodeRune(p []byte, r rune) int {
	switch {
	case r < 0x80:
		p[0] = byte(r)
		return 1
	case r < 0x800:
		p[0] = 0xC0 | byte(r>>6)
		p[1] = 0x80 | byte(r)&0x3F
		return 2
	case r < 0x10000:
		p[0] = 0xE0 | byte(r>>12)
		p[1] = 0x80 | byte(r>>6)&0x3F
		p[2] = 0x80 | byte(r)&0x3F
		return 3
	default:
		p[0] = 0xF0 | byte(r>>18)
		p[1] = 0x80 | byte(r>>12)&0x3F
		p[2] = 0x80 | byte(r>>6)&0x3F
		p[3] = 0x80 | byte(r)&0x3F
		return 4
	}
}

// AppendText appends text at the end of an edit or rich edit control.
func AppendText(hwnd HWND, text string) {
	// A position past the end is clamped to the end.
	end, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	SendMessage(hwnd, EM_SETSEL, end, end)
	SendMessage(hwnd, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(UTF16(text))))
	SendMessage(hwnd, EM_SCROLLCARET, 0, 0)
}

// Style returns the window style of hwnd.
func Style(hwnd HWND) uint32 {
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle)
	return uint32(r)
}

// SetStyle replaces the window style of hwnd.
func SetStyle(hwnd HWND, style uint32) {
	procSetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle, uintptr(style))
}

// SetTimer starts a timer that sends WM_TIMER with id every ms milliseconds.
func SetTimer(hwnd HWND, id uintptr, ms uint32) { procSetTimer.Call(uintptr(hwnd), id, uintptr(ms), 0) }

// KillTimer stops a timer.
func KillTimer(hwnd HWND, id uintptr) { procKillTimer.Call(uintptr(hwnd), id) }

// SetTextColor sets the text color of a device context (WM_CTLCOLORSTATIC).
func SetTextColor(hdc uintptr, color uint32) { procSetTextColor.Call(hdc, uintptr(color)) }

// BlockShutdown shows reason while Windows waits for the window before
// ending the session.
func BlockShutdown(hwnd HWND, reason string) {
	procShutdownBlockReasonCreate.Call(uintptr(hwnd), uintptr(unsafe.Pointer(UTF16(reason))))
}

// UnblockShutdown removes the reason set by BlockShutdown.
func UnblockShutdown(hwnd HWND) { procShutdownBlockReasonDestroy.Call(uintptr(hwnd)) }

// Task dialog icons and flags.
const (
	TD_WARNING_ICON     = 0xFFFF
	TD_ERROR_ICON       = 0xFFFE
	TD_INFORMATION_ICON = 0xFFFD

	tdfAllowCancellation     = 0x0008
	tdfUseCommandLinks       = 0x0010
	tdfPositionRelativeToWnd = 0x1000
)

// TaskButton is a custom task dialog button.
type TaskButton struct {
	ID   int32
	Text string
}

// TaskDialog describes a task dialog.
type TaskDialog struct {
	Title       string
	Instruction string // main instruction (large text)
	Content     string
	Icon        uintptr // TD_*_ICON or 0
	Buttons     []TaskButton
	Default     int32
	// Radios are radio buttons; the chosen ID is returned.
	Radios       []TaskButton
	DefaultRadio int32
	// CommandLinks shows the buttons as large command links.
	CommandLinks bool
}

// Show shows the dialog modal to owner and returns the ID of the button and
// the radio button chosen. Closing it with Esc or the close button returns
// IDCANCEL.
func (d TaskDialog) Show(owner HWND) (button, radio int32, err error) {
	// TASKDIALOGCONFIG and TASKDIALOG_BUTTON are declared with 1-byte
	// packing, so they are laid out by hand (x64: 160 and 12 bytes).
	var keep [][]uint16
	str := func(s string) uint64 {
		if s == "" {
			return 0
		}
		u, _ := windows.UTF16FromString(s)
		keep = append(keep, u)
		return uint64(uintptr(unsafe.Pointer(&u[0])))
	}
	buttonArray := func(buttons []TaskButton) []byte {
		if len(buttons) == 0 {
			return nil
		}
		b := make([]byte, 12*len(buttons))
		for i, btn := range buttons {
			binary.LittleEndian.PutUint32(b[12*i:], uint32(btn.ID))
			binary.LittleEndian.PutUint64(b[12*i+4:], str(btn.Text))
		}
		return b
	}
	ptr := func(b []byte) uint64 {
		if len(b) == 0 {
			return 0
		}
		return uint64(uintptr(unsafe.Pointer(&b[0])))
	}
	buttons := buttonArray(d.Buttons)
	radios := buttonArray(d.Radios)
	flags := uint32(tdfAllowCancellation | tdfPositionRelativeToWnd)
	if d.CommandLinks {
		flags |= tdfUseCommandLinks
	}

	cfg := make([]byte, 160)
	le := binary.LittleEndian
	le.PutUint32(cfg[0:], 160)
	le.PutUint64(cfg[4:], uint64(owner))
	le.PutUint64(cfg[12:], uint64(ModuleHandle()))
	le.PutUint32(cfg[20:], flags)
	le.PutUint32(cfg[24:], 0) // common buttons
	le.PutUint64(cfg[28:], str(d.Title))
	le.PutUint64(cfg[36:], uint64(d.Icon))
	le.PutUint64(cfg[44:], str(d.Instruction))
	le.PutUint64(cfg[52:], str(d.Content))
	le.PutUint32(cfg[60:], uint32(len(d.Buttons)))
	le.PutUint64(cfg[64:], ptr(buttons))
	le.PutUint32(cfg[72:], uint32(d.Default))
	le.PutUint32(cfg[76:], uint32(len(d.Radios)))
	le.PutUint64(cfg[80:], ptr(radios))
	le.PutUint32(cfg[88:], uint32(d.DefaultRadio))
	// Verification text, expanded information, footer, callback: unused.

	var b, r int32
	hr, _, _ := procTaskDialogIndirect.Call(uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&b)), uintptr(unsafe.Pointer(&r)), 0)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(buttons)
	runtime.KeepAlive(radios)
	runtime.KeepAlive(cfg)
	if hr != 0 {
		return 0, 0, fmt.Errorf("TaskDialogIndirect failed: 0x%08X", uint32(hr))
	}
	return b, r, nil
}

// gwlStyle is GWL_STYLE (-16) as the unsigned index GetWindowLongPtrW takes.
const gwlStyle = ^uintptr(15)

var procGetWindowRect = user32.NewProc("GetWindowRect")

// WindowRect returns the screen rectangle of hwnd.
func WindowRect(hwnd HWND) Rect {
	var r Rect
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}
