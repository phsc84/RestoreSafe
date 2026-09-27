package gui

import (
	"RestoreSafe/internal/ui"
	"context"
	"errors"
	"fmt"
	"strings"
)

// operation is one of the three workflows.
type operation int

const (
	opBackup operation = iota
	opRestore
	opVerify
)

// name returns the operation's name as the workflows use it in messages.
func (o operation) name() string {
	switch o {
	case opRestore:
		return "Restore"
	case opVerify:
		return "Verification"
	}
	return "Backup"
}

// title returns the operation's window title part.
func (o operation) title() string {
	switch o {
	case opRestore:
		return "Restore backup"
	case opVerify:
		return "Verify backup"
	}
	return "Create backup"
}

// outcome is what the result screen shows.
type outcome struct {
	status ui.Status
	text   string
	// showReport keeps the preflight report visible (the preflight blocked
	// the operation).
	showReport bool
}

// operationOutcome maps the end of a workflow to the result screen
// (docs/SPEC-restoresafe-gui.md, section 7.4). res is the result reported
// through ShowResult, or nil.
func operationOutcome(op operation, res *ui.Result, err error) outcome {
	name := op.name()
	switch {
	case err == nil && res != nil && res.Warnings > 0:
		return outcome{ui.StatusWarn, fmt.Sprintf("%s completed with %d warning(s). See the log.", name, res.Warnings), false}
	case err == nil && res != nil:
		return outcome{ui.StatusOK, name + " completed successfully.", false}
	case err == nil, errors.Is(err, ui.ErrCancelled):
		// The workflow ended before it started: cancelled by the user or
		// nothing to do; the log pane says which.
		return outcome{ui.StatusNone, name + " not started.", false}
	case errors.Is(err, context.Canceled):
		return outcome{ui.StatusWarn, err.Error() + " See the log for what was kept.", false}
	case strings.HasPrefix(err.Error(), name+" preflight failed:"):
		return outcome{ui.StatusError, name + " not started: the preflight found errors (see below).", true}
	}
	return outcome{ui.StatusError, fmt.Sprintf("%s failed: %v", name, err), false}
}
