package interact

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/naming"
	"time"
)

// The questions a workflow asks while it unlocks or creates keys
// (refactoring 2.0 RF-26). Each carries what a frontend needs to word it:
// the workflow writes no prompt and no message for it.

// SecretKind is the secret a SecretQuestion asks for.
type SecretKind int

const (
	// SecretPassword is the password; with password + YubiKey keys, the
	// YubiKey was touched before.
	SecretPassword SecretKind = iota
	// SecretRecoveryCode is the recovery code.
	SecretRecoveryCode
)

// OtherKeys names a key set that is not the first one an operation unlocks:
// a backup made with older keys needs their credentials (GUI spec CR-1).
type OtherKeys struct {
	// Set is the backup that needs these keys.
	Set naming.BackupEntry
	// Created is when the keys were created.
	Created time.Time
}

// Attempt says which try a question is and why the previous one failed.
type Attempt struct {
	// N counts the tries, from 1.
	N int
	// Left is the number of tries left, this one included.
	Left int
	// Failure is why the previous try failed; nil on the first.
	Failure error
}

// Retry reports whether the question is asked again after a failure.
func (a Attempt) Retry() bool { return a.N > 1 }

// SecretQuestion asks for the secret that unlocks a key set.
type SecretQuestion struct {
	Kind SecretKind
	// Action is the operation that needs the keys: "backup", "restore" or
	// "verification".
	Action string
	// Keys is set when the operation unlocked another key set before.
	Keys *OtherKeys
	Attempt
}

// UnlockChoice asks whether to unlock with the regular credentials of Mode
// or with the recovery code.
type UnlockChoice struct {
	Mode   config.AuthMode
	Action string
	// Keys is set when the operation unlocked another key set before.
	Keys *OtherKeys
}

// NewPasswordQuestion asks for the password of new keys and its
// confirmation.
type NewPasswordQuestion struct {
	// MinLength is the number of characters the password needs at least.
	MinLength int
	Attempt
}

// SpareQuestion asks to connect the spare YubiKey for its registration.
type SpareQuestion struct {
	Attempt
}
