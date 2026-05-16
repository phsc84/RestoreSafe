package operation

import (
	"fmt"
	"io"
)

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

// PrintYubiKeyPreflightStatus prints the YubiKey connection status line under
// the Authentication field. action is the operation label ("backup", "restore",
// "verification"). No output is produced when requiresYubiKey is false.
func PrintYubiKeyPreflightStatus(w io.Writer, requiresYubiKey bool, action string, checkYubiKeyConnected func() error) {
	if !requiresYubiKey {
		return
	}
	if err := checkYubiKeyConnected(); err != nil {
		fmt.Fprintf(w, "  [WARN] YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting %s.\n", action)
	} else {
		fmt.Fprintf(w, "  [OK] YubiKey connected. Keep it connected now before starting %s.\n", action)
	}
}
