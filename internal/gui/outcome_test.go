package gui

import (
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestOperationOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		op     operation
		res    *interact.Result
		err    error
		status interact.Status
		text   string
		report bool
	}{
		{opBackup, &interact.Result{LogPath: "x.log"}, nil, interact.StatusOK, "Backup completed successfully.", false},
		{opRestore, &interact.Result{Warnings: 2}, nil, interact.StatusWarn, "Restore completed with 2 warning(s). See the log.", false},
		{opVerify, nil, nil, interact.StatusNone, "Verification not started.", false},
		{opBackup, nil, fmt.Errorf("unlock: %w", interact.ErrCancelled), interact.StatusNone, "Backup not started.", false},
		{opBackup, nil, job.Cancelled("Backup"), interact.StatusWarn, "Backup cancelled. See the log", false},
		{opVerify, nil, errors.New("Verification preflight failed: 1 selected item(s) are invalid."), interact.StatusError, "Verification not started", true},
		{opRestore, nil, errors.New("Wrong password."), interact.StatusError, "Restore failed: Wrong password.", false},
	}
	for _, c := range cases {
		o := operationOutcome(c.op, c.res, c.err)
		if o.status != c.status || !strings.HasPrefix(o.text, c.text) || o.showReport != c.report {
			t.Errorf("%s / %v / %v: got %+v, want %v %q report=%v", c.op.name(), c.res, c.err, o, c.status, c.text, c.report)
		}
	}
}
