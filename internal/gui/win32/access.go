package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	clsidAccPropServices = windows.GUID{Data1: 0xB5F8350B, Data2: 0x0548, Data3: 0x48B1, Data4: [8]byte{0xA6, 0xEE, 0x88, 0xBD, 0x00, 0xB4, 0xA5, 0xE7}}
	iidIAccPropServices  = windows.GUID{Data1: 0x6E26E776, Data2: 0x04F0, Data3: 0x495D, Data4: [8]byte{0x80, 0xE4, 0x33, 0x30, 0x35, 0x2E, 0x31, 0x69}}
	propidAccName        = windows.GUID{Data1: 0x608D3DF8, Data2: 0x8128, Data3: 0x4AA7, Data4: [8]byte{0xA4, 0x28, 0xF5, 0x5E, 0x49, 0x26, 0x72, 0x91}}

	accPropServices comObject
)

const (
	vtSetHwndPropStr = 7          // IAccPropServices::SetHwndPropStr
	objidClient      = 0xFFFFFFFC // OBJID_CLIENT
	childidSelf      = 0
)

// SetAccessibleName gives a control the name screen readers announce, for
// controls whose window text is their content (rich edits) or that have no
// label (tree view, progress bar). Failures are ignored: the name is an
// aid, not a requirement. COM must be initialized (InitCOM).
func SetAccessibleName(hwnd HWND, name string) {
	if accPropServices == 0 {
		var svc comObject
		hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidAccPropServices)), 0, clsctxInprocServer,
			uintptr(unsafe.Pointer(&iidIAccPropServices)), uintptr(unsafe.Pointer(&svc)))
		if hr != 0 {
			return
		}
		// Kept for the lifetime of the process, with the annotations.
		accPropServices = svc
	}
	// MSAAPROPID is a GUID passed by value, which the x64 calling
	// convention passes as a pointer to a copy.
	prop := propidAccName
	accPropServices.call(vtSetHwndPropStr, uintptr(hwnd), objidClient, childidSelf,
		uintptr(unsafe.Pointer(&prop)), uintptr(unsafe.Pointer(UTF16(name))))
}
