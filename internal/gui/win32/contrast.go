package win32

import "unsafe"

var procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")

// System colors (GetSysColor) and messages about system settings.
const (
	COLOR_WINDOWTEXT    = 8
	COLOR_HIGHLIGHT     = 13
	COLOR_HIGHLIGHTTEXT = 14
	COLOR_BTNFACE       = 15
	COLOR_GRAYTEXT      = 17
	COLOR_BTNTEXT       = 18
	COLOR_HOTLIGHT      = 26
	WM_SETTINGCHANGE    = 0x001A
	WM_SYSCOLORCHANGE   = 0x0015

	spiGetHighContrast = 0x0042
	hcfHighContrastOn  = 0x1
)

// highContrast is HIGHCONTRASTW.
type highContrast struct {
	Size          uint32
	Flags         uint32
	DefaultScheme *uint16
}

// HighContrastOn reports whether Windows runs with a high-contrast theme.
func HighContrastOn() bool {
	hc := highContrast{}
	hc.Size = uint32(unsafe.Sizeof(hc))
	r, _, _ := procSystemParametersInfoW.Call(spiGetHighContrast, uintptr(hc.Size), uintptr(unsafe.Pointer(&hc)), 0)
	return r != 0 && hc.Flags&hcfHighContrastOn != 0
}
