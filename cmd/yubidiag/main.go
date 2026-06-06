//go:build windows

// yubidiag is a developer diagnostic tool that enumerates YubiKey USB
// interfaces and checks the Windows WebAuthn API used by RestoreSafe's FIDO2
// hmac-secret authentication.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unsafe"

	"RestoreSafe/internal/security"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	hidDLL                = windows.NewLazySystemDLL("hid.dll")
	procHidDGetAttributes = hidDLL.NewProc("HidD_GetAttributes")
)

type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

const (
	yubicoVID = 0x1050
)

type yubiInterface struct {
	rawPath     string
	pid         uint16
	firmware    string
	writeAccess bool
	openErr     error
}

func (yi yubiInterface) interfaceName() string {
	upper := strings.ToUpper(yi.rawPath)
	switch {
	case strings.Contains(upper, "MI_00"):
		return "OTP"
	case strings.Contains(upper, "MI_01"):
		return "FIDO2/U2F (used by RestoreSafe)"
	case strings.Contains(upper, "MI_02"):
		return "CCID / Smart Card"
	default:
		return "Unknown"
	}
}

func main() {
	fmt.Println("================================")
	fmt.Println("RestoreSafe YubiKey Diagnostic")
	fmt.Println("================================")
	fmt.Println()

	ifaces := findYubiKeyInterfaces()
	printResults(ifaces)

	fmt.Println()
	fmt.Println("Press Enter to exit...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func printResults(ifaces []yubiInterface) {
	if len(ifaces) == 0 {
		fmt.Println("[WARN] No YubiKey found in the Windows HID device list.")
		fmt.Println()
		fmt.Println("Suggestions:")
		fmt.Println("  - Make sure the YubiKey is physically connected.")
		fmt.Println("  - Try a different USB port.")
		fmt.Println("  - Unplug and re-insert the YubiKey, then run this tool again.")
		fmt.Println()
		fmt.Println("Verdict: YubiKey not detected.")
		fmt.Println("         RestoreSafe health check will report \"YubiKey not connected\".")
		return
	}

	var pid uint16
	var firmware string
	for _, iface := range ifaces {
		if iface.firmware != "" {
			pid = iface.pid
			firmware = iface.firmware
			break
		}
	}

	fmt.Println("YubiKey detected")
	if pid != 0 {
		fmt.Printf("  Model:    PID=0x%04X\n", pid)
	}
	if firmware != "" {
		fmt.Printf("  Firmware: %s\n", firmware)
	}
	fmt.Println()

	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].rawPath < ifaces[j].rawPath
	})

	fmt.Println("USB inventory:")
	for _, iface := range ifaces {
		if iface.writeAccess {
			fmt.Printf("  [INFO] %s - generic HID write access granted\n", iface.interfaceName())
		} else {
			fmt.Printf("  [INFO] %s - access denied (normal for non-FIDO2 interfaces)\n", iface.interfaceName())
		}
	}

	fmt.Println()
	fmt.Println("WebAuthn API:")
	webauthnErr := security.CheckYubiKeyAvailability()
	if webauthnErr == nil {
		fmt.Println("  [OK] Windows WebAuthn API (webauthn.dll) is present and supports hmac-secret.")
	} else {
		fmt.Printf("  [WARN] %v\n", webauthnErr)
	}

	fmt.Println()
	if webauthnErr != nil {
		fmt.Println("Verdict: YubiKey found but the Windows WebAuthn API is not available.")
		fmt.Println("         Remedy: Windows 11 version 22H2 or later is required.")
	} else {
		fmt.Println("Verdict: YubiKey detected and WebAuthn API is ready for preflight checks.")
		fmt.Println("         This does not prove the FIDO2 PIN or hmac-secret authentication path.")
		fmt.Println()
		runLiveProbe := promptYesNo("Run live FIDO2 hmac-secret test now? This shows Windows Security prompts. [y/N]: ")
		if runLiveProbe {
			printLiveFIDO2Probe()
		} else {
			fmt.Println("Live FIDO2 hmac-secret test skipped.")
		}
	}

	fmt.Println()
	fmt.Println("--- Technical details (include in bug reports) ---")
	for _, iface := range ifaces {
		fmt.Printf("  Interface: %s\n", iface.interfaceName())
		fmt.Printf("  Path:      %s\n", iface.rawPath)
		if iface.firmware != "" {
			fmt.Printf("  Firmware:  %s\n", iface.firmware)
		}
		if iface.writeAccess {
			fmt.Println("  Access:    generic HID write OK")
		} else if errors.Is(iface.openErr, windows.ERROR_ACCESS_DENIED) {
			fmt.Println("  Access:    generic HID denied")
		} else {
			fmt.Printf("  Access:    generic HID denied (%v)\n", iface.openErr)
		}
	}
	printRegistrySection()
}

func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

func printLiveFIDO2Probe() {
	fmt.Println()
	fmt.Println("Live FIDO2 hmac-secret test:")
	fmt.Println("  Windows will register a diagnostic credential, then ask again to derive a test secret.")
	combined, challengeJSON, err := security.CombineWithPassword([]byte("restoresafe-yubidiag"), false)
	if len(combined) > 0 {
		security.ZeroBytes(combined)
	}
	if err != nil {
		fmt.Printf("  [ERROR] %v\n", err)
		fmt.Println("Verdict: Live FIDO2 hmac-secret authentication failed.")
		fmt.Println("         Remedy: Ensure the YubiKey supports FIDO2 hmac-secret and has a FIDO2 PIN configured.")
		return
	}
	if err := security.ValidateChallengeJSON(challengeJSON); err != nil {
		fmt.Printf("  [ERROR] Diagnostic challenge validation failed: %v\n", err)
		fmt.Println("Verdict: Live FIDO2 hmac-secret authentication returned invalid challenge data.")
		return
	}
	fmt.Println("  [OK] FIDO2 credential creation and hmac-secret derivation succeeded.")
	fmt.Println("Verdict: RestoreSafe FIDO2 hmac-secret authentication should work.")
}

func printRegistrySection() {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\HID`,
		registry.READ|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	defer key.Close()

	subkeys, _ := key.ReadSubKeyNames(-1)
	for _, sk := range subkeys {
		if !strings.Contains(strings.ToUpper(sk), "VID_1050") {
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
				fmt.Printf("  Registry:  HID\\%s\\%s  Driver: %s\n", sk, inst, svc)
			}
		}
		devKey.Close()
	}
}

func findYubiKeyInterfaces() []yubiInterface {
	paths, err := security.YubiKeyHIDDevicePaths()
	if err != nil {
		fmt.Printf("[ERROR] HID device enumeration failed: %v\n", err)
		return nil
	}

	var result []yubiInterface
	for _, rawPath := range paths {
		iface := yubiInterface{rawPath: rawPath}
		h, err := windows.CreateFile(
			windows.StringToUTF16Ptr(rawPath),
			windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			nil, windows.OPEN_EXISTING, 0, 0,
		)
		if err != nil {
			iface.openErr = err
			h, err = windows.CreateFile(
				windows.StringToUTF16Ptr(rawPath),
				0,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
				nil, windows.OPEN_EXISTING, 0, 0,
			)
		} else {
			iface.writeAccess = true
		}

		if err == nil {
			attrs := hiddAttributes{Size: uint32(unsafe.Sizeof(hiddAttributes{}))}
			if r, _, _ := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attrs))); r != 0 {
				iface.pid = attrs.ProductID
				iface.firmware = decodeFirmware(attrs.VersionNumber)
			}
			windows.CloseHandle(h)
		}

		result = append(result, iface)
	}
	return result
}

func decodeFirmware(v uint16) string {
	return fmt.Sprintf("%d.%d.%d", (v>>8)&0xFF, (v>>4)&0xF, v&0xF)
}
