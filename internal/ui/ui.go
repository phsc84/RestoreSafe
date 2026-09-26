// Package ui defines the interface between the backup, restore, and verify
// workflows and the user. The workflows never read input or write to the
// console directly: every question goes through a UI method, and every
// message is written to UI.Output. Console implements it for the terminal; a
// graphical frontend implements the same interface.
//
// The workflows call a UI from the goroutine that runs the operation, and each
// question method blocks until the user has answered. A graphical
// implementation therefore runs the operation on a worker goroutine and
// forwards questions to its UI thread.
package ui

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/util"
	"errors"
	"io"
)

// ErrCancelled indicates that the user cancelled a selection.
var ErrCancelled = errors.New("selection cancelled")

// BackupStart is the answer to the question whether to start a backup.
type BackupStart int

const (
	// BackupCancel cancels the backup.
	BackupCancel BackupStart = iota
	// BackupAsPlanned starts the backup as shown in the preflight.
	BackupAsPlanned
	// BackupFull makes every directory a full backup with the current keys.
	BackupFull
	// BackupNewKeys creates new keys and makes every directory a full backup.
	BackupNewKeys
)

// BackupStartOptions says which alternatives the backup start question
// offers besides starting as planned and cancelling.
type BackupStartOptions struct {
	// OfferFull offers BackupFull; set when a differential is planned.
	OfferFull bool
	// OfferNewKeys offers BackupNewKeys; set when existing keys are reused.
	OfferNewKeys bool
}

// UI is what the workflows need from the user. Secrets are returned as byte
// slices that the caller zeroes after use.
type UI interface {
	// Output receives the text the user should see besides reports and
	// questions: log lines, notices, and results.
	Output() io.Writer

	// ShowReport shows the preflight summary of an operation before its
	// start is confirmed.
	ShowReport(r Report)

	// SelectBackups asks which backups to restore or verify (action is
	// "restore" or "verify"). runs is newest first and not empty. It returns
	// ErrCancelled when the user cancels.
	SelectBackups(action string, runs []catalog.BackupRunSummary) ([]util.BackupEntry, error)
	// RestoreDestination asks for the directory to restore into. It returns
	// ErrCancelled when the user cancels.
	RestoreDestination(backupDir string) (string, error)

	// ConfirmStart asks whether to start the action ("restore" or
	// "verification") shown in the preflight.
	ConfirmStart(action string) (bool, error)
	// ConfirmBackupStart asks whether to start the backup shown in the
	// preflight, offering the alternatives in opts.
	ConfirmBackupStart(opts BackupStartOptions) (BackupStart, error)

	// ChooseUnlockMethod asks whether to unlock with the regular credentials
	// (described by regular, e.g. "Password + YubiKey") or the recovery code.
	// It returns true for the recovery code.
	ChooseUnlockMethod(regular string) (bool, error)
	// Password asks for a secret (password or recovery code) without echo.
	Password(prompt string) ([]byte, error)
	// NewPassword asks for a new password and its confirmation. It returns
	// security.ErrPasswordEmpty or security.ErrPasswordMismatch when the user
	// can correct the input by trying again.
	NewPassword(prompt, confirmPrompt string) ([]byte, error)

	// ShowRecoveryCode shows a new recovery code. It is shown only this once.
	ShowRecoveryCode(code string)
	// RetypeRecoveryCode asks the user to type the recovery code shown by
	// ShowRecoveryCode, to confirm it was written down.
	RetypeRecoveryCode() (string, error)
	// WaitForSpareYubiKey waits until the user has connected the spare
	// YubiKey. It returns false when the user cancels.
	WaitForSpareYubiKey() (bool, error)
}
