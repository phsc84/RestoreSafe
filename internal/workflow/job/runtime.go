// Package job holds what backup, restore and verify runs share: the log
// file, progress tracking, cancellation, preflight rows, source directory
// checks, and the backup-directory lock of restore and verify.
package job

import (
	"context"
	"errors"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// OpenLogger opens the log file at logPath, falling back to a logger without
// a file when it cannot be opened, which it reports to u (LogNotOpened).
// Messages are mirrored to u's output.
func OpenLogger(cfg *config.Config, logPath string, u interact.UI) *logging.Logger {
	out := u.Output()
	log, err := logging.NewLogger(logPath, cfg.LogLevel, out)
	if err != nil {
		u.LogFailed(interact.LogFailure{Kind: interact.LogNotOpened, Err: err})
		return logging.NewOutputLogger(cfg.LogLevel, out)
	}
	return log
}

// CloseLog closes log and reports a problem with its file to u. It reports
// whether there was one, so that a completed run can count it as a warning
// before it shows its result. A second call does nothing, so a workflow
// closes the log before its result and defers the call for its error paths.
func CloseLog(u interact.UI, log *logging.Logger) bool {
	var fe *logging.FileError
	if err := log.Close(); !errors.As(err, &fe) {
		return false
	}
	kind := interact.LogIncomplete
	if fe.Kept != "" {
		kind = interact.LogKept
	}
	u.LogFailed(interact.LogFailure{Kind: kind, Err: fe.Err, Kept: fe.Kept})
	return true
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
