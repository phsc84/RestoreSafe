//go:build windows

package yubikey

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ── Diagnosis without prompts (GUI spec YK-3, YK-4, YK-9) ────────────────────

// fidoUsagePage is the HID usage page of the FIDO interface. Single-function
// keys have no MI_xx in their path, so the usage page is what finds it.
const fidoUsagePage = 0xF1D0

// Function is what a HID interface of a YubiKey does.
type Function int

const (
	FunctionOther Function = iota
	FunctionFIDO
	FunctionOTP
)

func (f Function) String() string {
	switch f {
	case FunctionFIDO:
		return "FIDO"
	case FunctionOTP:
		return "OTP"
	}
	return "other"
}

// Interface is one HID interface of a connected YubiKey.
type Interface struct {
	Path       string
	InstanceID string
	// Device is the USB device the interface belongs to; the interfaces of
	// one YubiKey share it.
	Device    string
	UsagePage uint16
	Usage     uint16
	ProductID uint16
	// Revision is the USB device version (bcdDevice).
	Revision uint16
	// WriteAccess tells whether Windows granted generic write access; OpenErr
	// is the error when it didn't.
	WriteAccess bool
	OpenErr     error
}

// Function returns what the interface does, from its HID usage page.
func (i Interface) Function() Function {
	switch {
	case i.UsagePage == fidoUsagePage:
		return FunctionFIDO
	case i.UsagePage == 0x01 && i.Usage == 0x06: // generic desktop, keyboard
		return FunctionOTP
	}
	return FunctionOther
}

// Key is one connected YubiKey: the interfaces of one USB device.
type Key struct {
	// Device is the instance ID of its USB device ("" when Windows didn't
	// name it).
	Device    string
	ProductID uint16
	Revision  uint16
	// FIDO tells whether one of its interfaces is a FIDO interface.
	FIDO       bool
	Interfaces []Interface
}

// DriverEntry is a HID driver entry of a Yubico device in the registry.
type DriverEntry struct {
	// Key is the entry below HKLM\SYSTEM\CurrentControlSet\Enum\HID.
	Key     string
	Service string
}

// Diagnosis is what Diagnose finds. It holds no secret and no credential.
type Diagnosis struct {
	// APIVersion is the WebAuthn API version of webauthn.dll (0 when it
	// can't be loaded), APIMinimum the version RestoreSafe needs, and APIErr
	// what Windows lacks: nil when the API is present and new enough for
	// hmac-secret salt values in GetAssertion.
	APIVersion uint32
	APIMinimum uint32
	APIErr     error
	// Interfaces are the Yubico HID interfaces, sorted by path; EnumErr is the
	// error of listing them.
	Interfaces []Interface
	EnumErr    error
	Drivers    []DriverEntry
}

// Keys returns the connected YubiKeys, in the order of their first
// interface.
func (d Diagnosis) Keys() []Key {
	var keys []Key
	index := map[string]int{}
	for _, iface := range d.Interfaces {
		device := yubiKeyOf(iface.Device, iface.InstanceID)
		i, ok := index[device]
		if !ok {
			keys = append(keys, Key{Device: iface.Device, ProductID: iface.ProductID, Revision: iface.Revision})
			i = len(keys) - 1
			index[device] = i
		}
		keys[i].Interfaces = append(keys[i].Interfaces, iface)
		if iface.Function() == FunctionFIDO {
			keys[i].FIDO = true
		}
	}
	return keys
}

// FIDO tells whether a connected YubiKey has a FIDO interface.
func (d Diagnosis) FIDO() bool {
	for _, iface := range d.Interfaces {
		if iface.Function() == FunctionFIDO {
			return true
		}
	}
	return false
}

// Seams of Diagnose, replaced in tests.
var (
	hidOpenFn    = openForWrite
	hidDriversFn = registryDrivers
)

// Diagnose finds what the YubiKey page shows, without any Windows Security
// prompt: the WebAuthn API, the Yubico HID interfaces and their drivers. It
// opens each interface for a moment only to learn whether write access is
// granted.
func Diagnose() Diagnosis {
	ver, loadErr := webauthnVersionFn()
	d := Diagnosis{APIVersion: ver, APIMinimum: webauthnMinAPIVersion, APIErr: webauthnVersionError(ver, loadErr)}
	devices, err := hidDevicesFn()
	d.EnumErr = err
	for _, dev := range devices {
		iface := Interface{Path: dev.Path, InstanceID: dev.InstanceID, Device: dev.Parent}
		parseHardwareIDs(&iface, dev.HardwareIDs)
		if err := hidOpenFn(dev.Path); err != nil {
			iface.OpenErr = err
		} else {
			iface.WriteAccess = true
		}
		d.Interfaces = append(d.Interfaces, iface)
	}
	sort.Slice(d.Interfaces, func(i, j int) bool { return d.Interfaces[i].Path < d.Interfaces[j].Path })
	d.Drivers = hidDriversFn()
	return d
}

// parseHardwareIDs reads the product ID, the USB device version and the HID
// usage page and usage from hardware IDs such as
// HID\VID_1050&PID_0407&REV_0543&MI_01 and HID_DEVICE_UP:F1D0_U:0001.
func parseHardwareIDs(iface *Interface, ids []string) {
	for _, id := range ids {
		for _, part := range strings.FieldsFunc(strings.ToUpper(id), func(r rune) bool { return r == '&' || r == '\\' || r == '_' }) {
			name, value, _ := strings.Cut(part, ":")
			switch name {
			case "UP":
				iface.UsagePage = hex16(value)
			case "U":
				iface.Usage = hex16(value)
			}
		}
		upper := strings.ToUpper(id)
		if v, ok := hexAfter(upper, "PID_"); ok {
			iface.ProductID = v
		}
		if v, ok := hexAfter(upper, "REV_"); ok {
			iface.Revision = v
		}
	}
}

// hexAfter returns the four hex digits that follow token in s.
func hexAfter(s, token string) (uint16, bool) {
	i := strings.Index(s, token)
	if i < 0 || len(s) < i+len(token)+4 {
		return 0, false
	}
	v, err := strconv.ParseUint(s[i+len(token):i+len(token)+4], 16, 16)
	return uint16(v), err == nil
}

func hex16(s string) uint16 {
	v, _ := strconv.ParseUint(s, 16, 16)
	return uint16(v)
}

// FormatRevision returns a USB device version as major.minor.patch, the way
// Yubico encodes firmware in it (0x0543 is 5.4.3).
func FormatRevision(v uint16) string {
	return fmt.Sprintf("%d.%d.%d", v>>8, (v>>4)&0xF, v&0xF)
}

// openForWrite opens a HID interface for generic write access and closes it
// again, to learn whether Windows grants it.
func openForWrite(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return err
	}
	return windows.CloseHandle(h)
}

// registryDrivers returns the HID driver entries of Yubico devices from
// HKLM\SYSTEM\CurrentControlSet\Enum\HID, sorted by key.
func registryDrivers() []DriverEntry {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\HID`, registry.READ|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer key.Close()

	vidToken := fmt.Sprintf("VID_%04X", yubicoVID)
	subkeys, _ := key.ReadSubKeyNames(-1)
	var entries []DriverEntry
	for _, sk := range subkeys {
		if !strings.Contains(strings.ToUpper(sk), vidToken) {
			continue
		}
		devKey, err := registry.OpenKey(key, sk, registry.READ|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		instances, _ := devKey.ReadSubKeyNames(-1)
		for _, inst := range instances {
			instKey, err := registry.OpenKey(devKey, inst, registry.READ)
			if err != nil {
				continue
			}
			svc, _, _ := instKey.GetStringValue("Service")
			instKey.Close()
			if svc != "" {
				entries = append(entries, DriverEntry{Key: sk + `\` + inst, Service: svc})
			}
		}
		devKey.Close()
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries
}
