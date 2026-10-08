// Package interact defines the interface between the backup, restore, and
// verify workflows and the user. The workflows never read input or write to the
// console directly: every question goes through a UI method, and every
// message is written to UI.Output. The window application implements it;
// interacttest.Script implements it for the tests of the workflows.
//
// The workflows call a UI from the goroutine that runs the operation, and each
// question method blocks until the user has answered. A graphical
// implementation therefore runs the operation on a worker goroutine and
// forwards questions to its UI thread. Progress arrives from a background
// goroutine while a step runs. To stop a running operation, the frontend
// cancels the context it passed to the workflow; a question is cancelled by
// returning ErrCancelled.
package interact

import (
	"errors"
	"io"
)

// ErrCancelled indicates that the user cancelled a question.
var ErrCancelled = errors.New("cancelled")

// BackupStart is the answer to the question whether to start a backup.
type BackupStart int

const (
	// BackupCancel cancels the backup.
	BackupCancel BackupStart = iota
	// BackupAsPlanned starts the backup of the plan shown.
	BackupAsPlanned
	// BackupFull makes every directory a full backup with the current keys.
	BackupFull
	// BackupNewKeys creates new keys and makes every directory a full backup.
	BackupNewKeys
	// BackupAutomatic returns to the automatic plan after BackupFull or
	// BackupNewKeys.
	BackupAutomatic
)

// BackupStartOptions says which answers the backup start question offers
// besides cancelling. BackupFull and BackupNewKeys show a new plan and ask
// again.
type BackupStartOptions struct {
	// Blocked is set when the plan has errors: BackupAsPlanned is not offered.
	Blocked bool
	// OfferFull offers BackupFull; set when a differential is planned.
	OfferFull bool
	// OfferNewKeys offers BackupNewKeys; set when existing keys are reused.
	OfferNewKeys bool
	// OfferAutomatic offers BackupAutomatic; set after the user chose
	// BackupFull or BackupNewKeys.
	OfferAutomatic bool
}

// UI is what the workflows need from the user. Secrets are returned as byte
// slices that the caller zeroes after use.
type UI interface {
	ProgressReporter

	// Output receives the text the user should see besides reports and
	// questions: log lines, notices, and results.
	Output() io.Writer

	// ShowBackupPlan, ShowRestorePlan and ShowVerifyPlan show what the
	// operation will do, before its start is confirmed.
	ShowBackupPlan(p BackupPlan)
	ShowRestorePlan(p RestorePlan)
	ShowVerifyPlan(p VerifyPlan)

	// LogStarted reports the log file the operation writes, as soon as it
	// is open: a frontend can offer it even when the operation fails.
	LogStarted(path string)

	// ShowResult shows the outcome of an operation that completed. It is
	// called last, before the workflow returns without error.
	ShowResult(r Result)

	// ConfirmStart asks whether to start the action ("restore" or
	// "verification") of the plan shown.
	ConfirmStart(action string) (bool, error)
	// ConfirmBackupStart asks whether to start the backup of the plan shown,
	// offering the answers in opts.
	ConfirmBackupStart(opts BackupStartOptions) (BackupStart, error)

	// ChooseUnlockMethod asks whether to unlock with the regular credentials
	// or the recovery code. It returns true for the recovery code.
	ChooseUnlockMethod(q UnlockChoice) (bool, error)
	// Password asks for a secret (password or recovery code) without echo.
	Password(q SecretQuestion) ([]byte, error)
	// NewPassword asks for a new password and its confirmation. It returns
	// ErrPasswordEmpty or ErrPasswordMismatch when the user
	// can correct the input by trying again.
	NewPassword(q NewPasswordQuestion) ([]byte, error)

	// ShowRecoveryCode shows a new recovery code. It is shown only this once.
	// It returns ErrCancelled when the user cancels instead of storing it.
	ShowRecoveryCode(code string) error
	// WaitForSpareYubiKey waits until the user has connected the spare
	// YubiKey. It returns false when the user cancels.
	WaitForSpareYubiKey(q SpareQuestion) (bool, error)
}
