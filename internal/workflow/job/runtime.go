// Package job holds what backup, restore and verify runs share: the log
// file, progress tracking, cancellation, preflight rows, source directory
// checks, and the backup-directory lock of restore and verify.
package job

import (
	"context"
	"fmt"
	"io"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
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
