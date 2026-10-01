package interact

// Code identifies a problem independently of its wording, so a frontend can
// choose its own text and the action that fixes it. Codes are stable: they
// are part of the contract between the workflows and the frontend.
type Code string

// Problems of the configuration, the folders and the backup directory, found
// by the health check or a preflight.
const (
	CodeConfigInvalid        Code = "CONFIG_INVALID"
	CodeBackupDirUnreachable Code = "BACKUP_DIR_UNREACHABLE"
	CodeBackupDirNotWritable Code = "BACKUP_DIR_NOT_WRITABLE"
	CodeSourceMissing        Code = "SOURCE_MISSING"
	CodeSourceInvalid        Code = "SOURCE_INVALID"
	CodeArgon2Capped         Code = "ARGON2_CAPPED"
	CodeYubiKeyNotConnected  Code = "YUBIKEY_NOT_CONNECTED"
	CodeNewKeysNeeded        Code = "NEW_KEYS_NEEDED"
	CodeOverdue              Code = "OVERDUE"
	CodeSpaceLow             Code = "SPACE_LOW"
	CodeLegacyBackups        Code = "LEGACY_1X"
	CodeLeftoverTempFiles    Code = "LEFTOVER_TMP"
	CodeLastBackupFailed     Code = "LAST_BACKUP_FAILED"
	CodeVerifyFailed         Code = "VERIFY_FAILED"
	CodeSkippedFiles         Code = "SKIPPED_FILES"
	CodeIncompleteNewest     Code = "INCOMPLETE_NEWEST"
	CodeBaseMissing          Code = "BASE_MISSING"
	CodeSetIncomplete        Code = "SET_INCOMPLETE"
	CodeSpaceInsufficient    Code = "SPACE_INSUFFICIENT"
	CodeSpaceEstimateOnly    Code = "SPACE_ESTIMATE_ONLY"
	CodePartLimit            Code = "PART_LIMIT"
	CodeFreeSpaceUnknown     Code = "FREE_SPACE_UNKNOWN"
	CodeRestoreTargetExists  Code = "RESTORE_TARGET_EXISTS"
	CodeRestoreTargetInvalid Code = "RESTORE_TARGET_INVALID"
)
