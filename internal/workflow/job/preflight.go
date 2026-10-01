package job

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
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

// UnlockPlan describes how ks is unlocked: its regular way and whether the
// recovery code opens it too.
func UnlockPlan(ks *container.KeySet) interact.UnlockPlan {
	return interact.UnlockPlan{
		Methods:      config.AuthMode(ks.AuthMode).Label(),
		RecoveryCode: ks.HasSlotType(container.SlotRecovery),
	}
}

// SetPlan describes a chosen backup set of a restore or verify: base is the
// full backup read with a differential (nil for a full backup), bytes the
// size read, and err the problem that keeps the set from being used.
func SetPlan(entry naming.BackupEntry, base *catalog.SetInfo, bytes int64, err error) interact.SetPlan {
	p := interact.SetPlan{Set: entry, Bytes: bytes}
	if base != nil {
		p.Base = base.Entry
	}
	if err != nil {
		p.Problem = err.Error()
	}
	return p
}
