package interact

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"time"
)

// BackupPlan is what a backup run will do. The workflow shows it before it
// asks whether to start, and again after the user chose another plan.
type BackupPlan struct {
	Folders   []FolderPlan
	BackupDir string
	// NeededBytes is the space the run likely needs; AllBytes the space if
	// every differential stored all its files again. They are equal when
	// no differential is planned.
	NeededBytes, AllBytes int64
	// FreeBytes is the free space in the backup directory, -1 when unknown.
	FreeBytes int64
	Keys      KeyPlan
	// VerifyAfter is set when each new backup is verified right after it is
	// written.
	VerifyAfter bool
	// Removes are the backup sets retention removes if the run succeeds.
	Removes []catalog.SetInfo
	// FullRequested is set when the user chose full backups or new keys
	// instead of the automatic plan.
	FullRequested bool
	// Issues block the start (StatusError) or deserve attention.
	Issues []Issue
	// Details is the complete preflight as text.
	Details Report
}

// HasErrors reports whether an issue blocks the backup.
func (p BackupPlan) HasErrors() bool { return hasErrors(p.Issues) }

// FolderPlan is what the run does with one source folder.
type FolderPlan struct {
	// Name is the folder's name in the backup file names.
	Name string
	Path string
	// Differential is set for a differential backup of the full backup Base;
	// otherwise the folder gets a full backup.
	Differential bool
	DiffNumber   int
	Base         naming.BackupEntry
	BaseCreated  time.Time
	// Reason explains why the folder gets this type.
	Reason string
	// EstimatedBytes is what the run likely stores for the folder, AllBytes
	// the size of all its files.
	EstimatedBytes, AllBytes int64
	// Skipped is set for an identical duplicate of another folder, which is
	// not backed up again.
	Skipped bool
	Warning string
	// Problem is set when the folder cannot be backed up.
	Problem string
}

// KeyPlan says which keys lock the new backups and what the user is asked
// for to unlock or create them.
type KeyPlan struct {
	// New is set when the run creates new keys; NewKeysReason says why.
	New           bool
	NewKeysReason string
	// Created and Summary describe the existing keys.
	Created time.Time
	Summary string
	// Password is set when a password is asked (twice for new keys).
	Password bool
	// YubiKeys counts the YubiKeys to touch (existing keys) or to register
	// (new keys).
	YubiKeys int
	// RecoveryCode is set when new keys come with a recovery code to write
	// down.
	RecoveryCode bool
}

// UnlockPlan describes how the keys of the chosen backups are unlocked.
type UnlockPlan struct {
	// Methods is the regular way, e.g. "Password + YubiKey".
	Methods string
	// Password and YubiKey are the prompts of the regular way: a password,
	// a YubiKey touch, or both.
	Password, YubiKey bool
	// RecoveryCode is set when the recovery code opens the keys too.
	RecoveryCode bool
}

// SetPlan is one chosen backup set of a restore or verify.
type SetPlan struct {
	Set naming.BackupEntry
	// Base is the full backup read together with a differential; zero for a
	// full backup.
	Base naming.BackupEntry
	// Bytes is the size read (the set and its base).
	Bytes int64
	// Problem is set when the set cannot be used.
	Problem string
}

// RestoreSetPlan is one chosen backup set of a restore and the folder it is
// restored into.
type RestoreSetPlan struct {
	SetPlan
	OutputDir string
	// OutputProblem is set when the folder cannot be created (e.g. it
	// exists).
	OutputProblem string
	// OutputCode classifies OutputProblem: RESTORE_TARGET_EXISTS or
	// RESTORE_TARGET_INVALID.
	OutputCode Code
}

// RestorePlan is what a restore will do, shown before the start is
// confirmed.
type RestorePlan struct {
	Sets        []RestoreSetPlan
	Destination string
	// NeededBytes estimates the space the restored folders take; FreeBytes
	// is the free space at the destination, -1 when unknown.
	NeededBytes, FreeBytes int64
	Unlock                 UnlockPlan
	Issues                 []Issue
	Details                Report
}

// HasErrors reports whether an issue blocks the restore.
func (p RestorePlan) HasErrors() bool { return hasErrors(p.Issues) }

// VerifyPlan is what a verification will check, shown before the start is
// confirmed.
type VerifyPlan struct {
	Sets []SetPlan
	// Bytes is the size read.
	Bytes   int64
	Unlock  UnlockPlan
	Issues  []Issue
	Details Report
}

// HasErrors reports whether an issue blocks the verification.
func (p VerifyPlan) HasErrors() bool { return hasErrors(p.Issues) }
