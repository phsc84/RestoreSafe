package gui

import "RestoreSafe/internal/startup"

// homeState is what the home screen shows besides the paths: whether the
// health check is running, and its result.
type homeState struct {
	checking bool
	health   *startup.HealthCheckResult
}

// actionsEnabled returns which of backup, restore, and verify can start.
func (s homeState) actionsEnabled() (backup, restoreOrVerify bool) {
	if s.checking || s.health == nil {
		return false, false
	}
	return !s.health.BlocksBackup(), !s.health.BlocksRestoreOrVerify()
}

// statusLine is the line under the report: why actions are blocked, or a
// hint about changing the configuration.
func (s homeState) statusLine() string {
	if s.checking || s.health == nil {
		return "Running the startup health check ..."
	}
	backup, restoreOrVerify := s.actionsEnabled()
	switch {
	case !backup && !restoreOrVerify:
		return "Backup, restore, and verify are blocked by the errors above. Fix them, then click Recheck."
	case !backup:
		return "Backup is blocked by the errors above. Fix them, then click Recheck."
	case !restoreOrVerify:
		return "Restore and verify are blocked by the errors above. Fix them, then click Recheck."
	}
	return "Changes to config.yaml take effect after restarting RestoreSafe."
}
