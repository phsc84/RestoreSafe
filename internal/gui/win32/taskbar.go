package win32

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	clsidTaskbarList   = windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidITaskbarList3   = windows.GUID{Data1: 0xEA1AFB91, Data2: 0x9E28, Data3: 0x4B86, Data4: [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
	procFlashWindowEx  = user32.NewProc("FlashWindowEx")
	procGetForeground  = user32.NewProc("GetForegroundWindow")
	procRegisterWinMsg = user32.NewProc("RegisterWindowMessageW")
)

// Taskbar progress states (TBPF_*).
const (
	TaskbarNoProgress    = 0x0
	TaskbarIndeterminate = 0x1
	TaskbarNormal        = 0x2
	TaskbarError         = 0x4
	TaskbarPaused        = 0x8
)

// Vtable indexes of ITaskbarList3 (IUnknown, ITaskbarList, ITaskbarList2).
const (
	vtHrInit           = 3
	vtSetProgressValue = 9
	vtSetProgressState = 10
)

// Taskbar is the taskbar button of a window: progress (ITaskbarList3).
type Taskbar struct {
	list comObject
	hwnd HWND
}

// NewTaskbar connects to the taskbar button of hwnd. Call it after the
// window received the TaskbarButtonCreated message (TaskbarButtonCreated).
func NewTaskbar(hwnd HWND) (*Taskbar, error) {
	var list comObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidITaskbarList3)), uintptr(unsafe.Pointer(&list)))
	if hr != 0 {
		return nil, fmt.Errorf("CoCreateInstance(TaskbarList) failed: 0x%08X", uint32(hr))
	}
	if hr := list.call(vtHrInit); hr != 0 {
		list.release()
		return nil, fmt.Errorf("ITaskbarList3.HrInit failed: 0x%08X", uint32(hr))
	}
	return &Taskbar{list: list, hwnd: hwnd}, nil
}

// SetState sets the progress state (Taskbar*).
func (t *Taskbar) SetState(state uint32) {
	if t != nil {
		t.list.call(vtSetProgressState, uintptr(t.hwnd), uintptr(state))
	}
}

// SetValue shows done of total on the button.
func (t *Taskbar) SetValue(done, total uint64) {
	if t != nil {
		t.list.call(vtSetProgressValue, uintptr(t.hwnd), uintptr(done), uintptr(total))
	}
}

// Release disconnects from the taskbar.
func (t *Taskbar) Release() {
	if t != nil {
		t.list.release()
		t.list = 0
	}
}

// TaskbarButtonCreated returns the message Windows sends when the window's
// taskbar button exists (again, e.g. after Explorer restarted).
func TaskbarButtonCreated() uint32 {
	r, _, _ := procRegisterWinMsg.Call(uintptr(unsafe.Pointer(UTF16("TaskbarButtonCreated"))))
	return uint32(r)
}

// flashWInfo is FLASHWINFO.
type flashWInfo struct {
	Size    uint32
	Hwnd    HWND
	Flags   uint32
	Count   uint32
	Timeout uint32
}

const (
	flashwAll       = 0x3
	flashwTimerNoFG = 0xC
)

// FlashUntilActive flashes the window's taskbar button until the window is
// activated.
func FlashUntilActive(hwnd HWND) {
	fi := flashWInfo{Hwnd: hwnd, Flags: flashwAll | flashwTimerNoFG}
	fi.Size = uint32(unsafe.Sizeof(fi))
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

// IsForeground reports whether hwnd is the foreground window.
func IsForeground(hwnd HWND) bool {
	r, _, _ := procGetForeground.Call()
	return HWND(r) == hwnd
}
