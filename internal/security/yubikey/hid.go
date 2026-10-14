//go:build windows

package yubikey

//lint:file-ignore ST1003 names follow setupapi.h, so they match the Windows API documentation

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── HID presence check (SetupDi) ─────────────────────────────────────────────

var (
	setupapiDLL = windows.NewLazySystemDLL("setupapi.dll")

	procSetupDiGetClassDevsW             = setupapiDLL.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = setupapiDLL.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = setupapiDLL.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = setupapiDLL.NewProc("SetupDiDestroyDeviceInfoList")
)

var guidDevInterfaceHID = windows.GUID{
	Data1: 0x4D1E55B2,
	Data2: 0xF16F,
	Data3: 0x11CF,
	Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30},
}

const (
	digcfPresent         = 0x00000002
	digcfDeviceInterface = 0x00000010
	yubicoVID            = 0x1050
)

type spDeviceInterfaceData struct {
	Size               uint32
	InterfaceClassGuid windows.GUID
	Flags              uint32
	Reserved           uintptr
}

// yubiKeyVIDVisible returns true if any HID device path contains the Yubico VID.
// Used as a lightweight presence check that does not open any device handle.
func yubiKeyVIDVisible() bool {
	paths, err := HIDDevicePaths()
	return err == nil && len(paths) > 0
}

func hidDevicePathsForVID(vendorID uint16) ([]string, error) {
	devInfo, _, _ := procSetupDiGetClassDevsW.Call(
		uintptr(unsafe.Pointer(&guidDevInterfaceHID)),
		0, 0,
		uintptr(digcfPresent|digcfDeviceInterface),
	)
	if devInfo == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevsW failed")
	}
	defer procSetupDiDestroyDeviceInfoList.Call(devInfo)

	vidToken := fmt.Sprintf("VID_%04X", vendorID)
	var paths []string
	for idx := uint32(0); idx < 1000; idx++ {
		var ifData spDeviceInterfaceData
		ifData.Size = uint32(unsafe.Sizeof(ifData))
		ret, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			devInfo, 0,
			uintptr(unsafe.Pointer(&guidDevInterfaceHID)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifData)),
		)
		if ret == 0 {
			break
		}
		path, err := deviceInterfacePath(devInfo, &ifData)
		if err == nil && strings.Contains(strings.ToUpper(path), vidToken) {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func deviceInterfacePath(devInfo uintptr, ifData *spDeviceInterfaceData) (string, error) {
	var requiredSize uint32
	procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfo,
		uintptr(unsafe.Pointer(ifData)),
		0, 0,
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)
	if requiredSize == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetail: zero size")
	}
	buf := make([]byte, requiredSize)
	if len(buf) < 4 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetail: buffer too small")
	}
	// cbSize must be 8 on 64-bit Windows (pack(1) in SDK header notwithstanding).
	*(*uint32)(unsafe.Pointer(&buf[0])) = 8
	ret, _, err := procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfo,
		uintptr(unsafe.Pointer(ifData)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(requiredSize),
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)
	if ret == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetail: %w", err)
	}
	pathUTF16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), (len(buf)-4)/2)
	return windows.UTF16ToString(pathUTF16), nil
}
