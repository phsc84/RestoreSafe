package win32

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                           = windows.NewLazySystemDLL("ole32.dll")
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = ole32.NewProc("CoTaskMemFree")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIShellItem       = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	clsctxInprocServer      = 0x1

	fosPickFolders     = 0x00000020
	fosForceFileSystem = 0x00000040
	fosPathMustExist   = 0x00000800
	sigdnFileSysPath   = 0x80058000
	hresultCancelled   = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	sFalse             = 1

	// Vtable indexes (IUnknown, IModalWindow, IFileDialog, IShellItem).
	vtRelease        = 2
	vtShow           = 3
	vtSetOptions     = 9
	vtGetOptions     = 10
	vtSetFolder      = 12
	vtSetTitle       = 17
	vtGetResult      = 20
	vtGetDisplayName = 5
)

// InitCOM initializes COM for the calling (UI) thread; the folder picker
// needs it.
func InitCOM() error {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	if hr != 0 && hr != sFalse {
		return fmt.Errorf("CoInitializeEx failed: 0x%08X", uint32(hr))
	}
	return nil
}

// comObject is a COM interface pointer.
type comObject uintptr

// call calls vtable method index with the object as first argument.
func (o comObject) call(index int, args ...uintptr) uintptr {
	// The object's first word points to its vtable.
	obj := *(*unsafe.Pointer)(unsafe.Pointer(&o))
	vtbl := *(**[32]uintptr)(obj)
	r, _, _ := syscall.SyscallN(vtbl[index], append([]uintptr{uintptr(o)}, args...)...)
	return r
}

func (o comObject) release() {
	if o != 0 {
		o.call(vtRelease)
	}
}

// PickFolder shows the Windows folder picker owned by owner, starting in
// start (if it exists). It returns the chosen folder, or ok=false when the
// user cancelled.
func PickFolder(owner HWND, title, start string) (path string, ok bool, err error) {
	var dialog comObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dialog)))
	if hr != 0 {
		return "", false, fmt.Errorf("Creating the folder picker failed: 0x%08X", uint32(hr))
	}
	defer dialog.release()

	var options uint32
	dialog.call(vtGetOptions, uintptr(unsafe.Pointer(&options)))
	dialog.call(vtSetOptions, uintptr(options|fosPickFolders|fosForceFileSystem|fosPathMustExist))
	dialog.call(vtSetTitle, uintptr(unsafe.Pointer(UTF16(title))))
	if start != "" {
		var folder comObject
		if hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(UTF16(start))), 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&folder))); hr == 0 {
			dialog.call(vtSetFolder, uintptr(folder))
			folder.release()
		}
	}

	switch hr := uint32(dialog.call(vtShow, uintptr(owner))); hr {
	case 0:
	case hresultCancelled:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("The folder picker failed: 0x%08X", hr)
	}
	var item comObject
	if hr := dialog.call(vtGetResult, uintptr(unsafe.Pointer(&item))); hr != 0 {
		return "", false, fmt.Errorf("Reading the chosen folder failed: 0x%08X", uint32(hr))
	}
	defer item.release()
	var name *uint16
	if hr := item.call(vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); hr != 0 {
		return "", false, fmt.Errorf("Reading the chosen folder failed: 0x%08X", uint32(hr))
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	return windows.UTF16PtrToString(name), true, nil
}
