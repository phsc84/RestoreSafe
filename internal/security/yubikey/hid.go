//go:build windows

package yubikey

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── HID enumeration (SetupDi, configuration manager) ─────────────────────────

var (
	procCMGetParent   = windows.NewLazySystemDLL("cfgmgr32.dll").NewProc("CM_Get_Parent")
	procCMGetDeviceID = windows.NewLazySystemDLL("cfgmgr32.dll").NewProc("CM_Get_Device_IDW")
)

var guidDevInterfaceHID = windows.GUID{
	Data1: 0x4D1E55B2,
	Data2: 0xF16F,
	Data3: 0x11CF,
	Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30},
}

const yubicoVID = 0x1050

// hidDevice is a Yubico HID interface as Windows lists it, before it is
// interpreted. Nothing in it needs the device to be opened.
type hidDevice struct {
	// Path is the device interface path, as CreateFile takes it.
	Path string
	// InstanceID is the HID device's instance ID, as in the registry.
	InstanceID string
	// Parent is the instance ID of the USB device the interface belongs to;
	// the interfaces of one YubiKey share it.
	Parent string
	// HardwareIDs carry the product ID, the USB device version (REV_) and the
	// HID usage page (UP:) and usage (U:).
	HardwareIDs []string
}

// hidDevicesFn lists the connected Yubico HID interfaces; replaced in tests.
var hidDevicesFn = yubicoHIDDevices

// yubiKeyVIDVisible returns true if any HID device path contains the Yubico VID.
// Used as a lightweight presence check that does not open any device handle.
func yubiKeyVIDVisible() bool {
	devices, err := hidDevicesFn()
	return err == nil && len(devices) > 0
}

func yubicoHIDDevices() ([]hidDevice, error) {
	set, err := windows.SetupDiGetClassDevsEx(&guidDevInterfaceHID, "", 0, windows.DIGCF_PRESENT|windows.DIGCF_DEVICEINTERFACE, 0, "")
	if err != nil {
		return nil, fmt.Errorf("SetupDiGetClassDevsEx: %w", err)
	}
	defer set.Close()

	vidToken := fmt.Sprintf("VID_%04X", yubicoVID)
	var devices []hidDevice
	for i := 0; ; i++ {
		data, err := set.EnumDeviceInfo(i)
		if err == windows.ERROR_NO_MORE_ITEMS {
			break
		}
		if err != nil {
			continue
		}
		id, err := set.DeviceInstanceID(data)
		if err != nil || !strings.Contains(strings.ToUpper(id), vidToken) {
			continue
		}
		paths, err := windows.CM_Get_Device_Interface_List(id, &guidDevInterfaceHID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
		if err != nil || len(paths) == 0 {
			continue
		}
		dev := hidDevice{Path: paths[0], InstanceID: id, Parent: usbParent(data.DevInst)}
		if v, err := set.DeviceRegistryProperty(data, windows.SPDRP_HARDWAREID); err == nil {
			dev.HardwareIDs, _ = v.([]string)
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

// usbParent returns the instance ID of the USB device above a HID device:
// the first ancestor under USB\ that isn't one interface of a composite
// device (MI_xx). It returns "" when there is none.
func usbParent(inst windows.DEVINST) string {
	for range 4 {
		var parent windows.DEVINST
		if r, _, _ := procCMGetParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(inst), 0); r != 0 {
			return ""
		}
		id := deviceID(parent)
		upper := strings.ToUpper(id)
		if strings.HasPrefix(upper, `USB\`) && !strings.Contains(upper, "&MI_") {
			return id
		}
		inst = parent
	}
	return ""
}

func deviceID(inst windows.DEVINST) string {
	buf := make([]uint16, windows.MAX_DEVICE_ID_LEN+1)
	if r, _, _ := procCMGetDeviceID.Call(uintptr(inst), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0); r != 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}
