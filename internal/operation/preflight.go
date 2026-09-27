package operation

import (
	"RestoreSafe/internal/workflow/interact"
	"fmt"
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

// AuthRows returns the preflight's Authentication field and, when a YubiKey
// is required, whether it is connected. action is the operation label
// ("backup", "restore", "verification").
func AuthRows(authLabel string, requiresYubiKey bool, action string, checkYubiKeyConnected func() error) []interact.Row {
	rows := []interact.Row{interact.Field("Authentication", authLabel)}
	if !requiresYubiKey {
		return rows
	}
	if err := checkYubiKeyConnected(); err != nil {
		return append(rows, interact.Item(interact.StatusWarn, fmt.Sprintf("YubiKey not connected. Remedy: Connect the YubiKey before starting %s.", action)))
	}
	return append(rows, interact.Item(interact.StatusOK, fmt.Sprintf("YubiKey connected. Keep it connected before starting %s.", action)))
}
