package job

import (
	"fmt"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// ValidatePreflightItems returns a formatted error when one or more items fail
// a caller-supplied validity check: failureTemplate (with %d for their
// number) and what the user does about it.
func ValidatePreflightItems[T any](items []T, hasError func(T) bool, failureTemplate, remedy string) error {
	invalid := 0
	for _, item := range items {
		if hasError(item) {
			invalid++
		}
	}
	if invalid > 0 {
		return problem.Errorf(failureTemplate, invalid).WithRemedy(remedy)
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
	mode := ks.AuthMode
	return interact.UnlockPlan{
		Methods:      mode.Label(),
		Password:     mode != config.AuthModeYubiKey,
		YubiKey:      mode == config.AuthModePasswordYubiKey || mode == config.AuthModeYubiKey,
		RecoveryCode: ks.HasSlotType(container.SlotRecovery),
	}
}
