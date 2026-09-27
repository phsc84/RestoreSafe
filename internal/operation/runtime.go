package operation

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"context"
	"fmt"
	"io"
)

// OpenLogger opens the log file of the backup run date/runID in backupDir,
// falling back to a console-only logger when the file cannot be opened.
// Messages are mirrored to console.
func OpenLogger(cfg *config.Config, backupDir, date string, runID naming.BackupID, console io.Writer) *logging.Logger {
	logPath := naming.LogFileName(backupDir, date, runID)
	log, err := logging.NewLogger(logPath, cfg.LogLevel, console)
	if err != nil {
		fmt.Fprintf(console, "Warning: Failed to open log file: %v. Remedy: Check write permissions in backup directory; operation continues without a log file.\n", err)
		return logging.NewConsoleLogger(cfg.LogLevel, console)
	}
	return log
}

func PasswordFailurePrefix(requiresYubiKey, yubiKeyOnly bool) string {
	switch {
	case yubiKeyOnly:
		return "Wrong YubiKey or corrupted file."
	case requiresYubiKey:
		return "Wrong password or invalid YubiKey response."
	default:
		return "Wrong password."
	}
}

// cancelledError reports an operation the user cancelled while it ran.
type cancelledError struct{ message string }

func (e cancelledError) Error() string { return e.message }

// Is makes a cancelled operation match context.Canceled.
func (e cancelledError) Is(target error) bool { return target == context.Canceled }

// Cancelled returns the error of an operation the user cancelled, e.g.
// Cancelled("Backup") reports "Backup cancelled.". It matches
// context.Canceled, so frontends can tell it from a failure.
func Cancelled(action string) error {
	return cancelledError{message: action + " cancelled."}
}
