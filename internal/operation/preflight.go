package operation

import (
	"fmt"
	"os"
)

// PreflightFieldLabelWidth is the standard label column width for preflight summary fields.
const PreflightFieldLabelWidth = 14

// ValidatePreflightItems returns a formatted error when one or more items fail
// a caller-supplied validity check.
func ValidatePreflightItems[T any](items []T, hasError func(T) bool, failureTemplate string) error {
	invalid := 0
	for _, item := range items {
		if hasError(item) {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf(failureTemplate, invalid)
	}
	return nil
}

// PrintPreflightField prints an aligned key/value field for preflight summaries.
func PrintPreflightField(labelWidth int, label, value string) {
	fmt.Printf("%-*s: %s\n", labelWidth, label, value)
}

// PrintYubiKeyPreflightStatus prints the YubiKey connection status line under
// the Authentication field. action is the operation label ("backup", "restore",
// "verification"). No output is produced when requiresYubiKey is false.
func PrintYubiKeyPreflightStatus(requiresYubiKey bool, action string, checkYubiKeyConnected func() error) {
	if !requiresYubiKey {
		return
	}
	if err := checkYubiKeyConnected(); err != nil {
		fmt.Printf("  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting %s.\n", action)
	} else {
		fmt.Printf("  [OK] YubiKey connected. Keep it connected now before starting %s.\n", action)
	}
}

// PrintKeyfilePreflightStatus prints the keyfile availability status line under
// the Authentication field. No output is produced when requiresKeyfile is false.
func PrintKeyfilePreflightStatus(requiresKeyfile bool, keyfilePath string) {
	if !requiresKeyfile {
		return
	}
	if _, err := os.Stat(keyfilePath); err != nil {
		fmt.Printf("  [WARN] Keyfile not found at %q. Remedy: Ensure the keyfile is available at the configured keyfile_path before starting the operation.\n", keyfilePath)
	} else {
		fmt.Printf("  [OK] Keyfile found: %s\n", keyfilePath)
	}
}
