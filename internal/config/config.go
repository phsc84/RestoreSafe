// Package config loads and validates config.yaml, including the exclude
// patterns.
package config

import (
	"RestoreSafe/internal/security/cryptox"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// AuthMode represents the authentication mode for backup operations.
type AuthMode int

// AuthMode values.
const (
	AuthModePassword        AuthMode = 1 // password only
	AuthModePasswordYubiKey AuthMode = 2 // password + YubiKey FIDO2 hmac-secret
	AuthModeYubiKey         AuthMode = 3 // YubiKey only, no password
)

// Label returns a human-readable description of the authentication mode.
// It is the single source of truth for these labels; callers that only know the
// authentication factors (e.g. restore/verify, which read them from a backup's
// challenge file) build an AuthMode via AuthModeFromFactors first.
func (a AuthMode) Label() string {
	switch a {
	case AuthModeYubiKey:
		return "YubiKey only (no password)"
	case AuthModePasswordYubiKey:
		return "password + YubiKey"
	default:
		return "password only"
	}
}

// AuthModeFromFactors classifies the authentication factors of a backup run into
// an AuthMode. usesYubiKey reports whether the run is protected by a YubiKey at
// all; noPassword reports whether the YubiKey is the sole factor. noPassword is
// only meaningful when usesYubiKey is true.
func AuthModeFromFactors(usesYubiKey, noPassword bool) AuthMode {
	switch {
	case !usesYubiKey:
		return AuthModePassword
	case noPassword:
		return AuthModeYubiKey
	default:
		return AuthModePasswordYubiKey
	}
}

// Argon2 parameter bounds, derived from the canonical bounds in package
// security (which owns the key-derivation function and on-disk header format).
// Memory is converted from package cryptox's KiB to the MB unit used in
// config.yaml.
//
// Values below the minimums are rejected by validate(). Values above the
// maximums are clamped to the maximum (with a startup warning) rather than
// rejected, so an over-aggressive config still runs and stays restorable. The
// threads maximum also keeps the value within the uint8 range Argon2 requires,
// preventing a silent overflow when the value is cast for key derivation.
const (
	Argon2MinTime     = cryptox.MinArgonTime
	Argon2MinMemoryMB = cryptox.MinArgonMemoryKB / 1024
	Argon2MinThreads  = cryptox.MinArgonThreads

	Argon2MaxTime     = cryptox.MaxArgonTime
	Argon2MaxMemoryMB = cryptox.MaxArgonMemoryKB / 1024
	Argon2MaxThreads  = cryptox.MaxArgonThreads
)

// Argon2 holds the Argon2id key-derivation tuning knobs exposed in config.yaml.
//
// What these parameters do:
//   - Time: number of passes over memory (more = slower to brute-force, slower to run).
//   - MemoryMB: working memory in megabytes (more = harder for GPUs, more RAM used).
//   - Threads: parallel lanes (should match physical CPU cores; beyond that, no benefit).
//
// Minimums (enforced by validation): Time ≥ 2, MemoryMB ≥ 64, Threads ≥ 1.
// Maximums (clamped with a warning): Time ≤ 20, MemoryMB ≤ 4096, Threads ≤ 255.
// Defaults: Time = 3, MemoryMB = 512, Threads = 4.
type Argon2 struct {
	Time     int `yaml:"time"`
	MemoryMB int `yaml:"memory_mb"`
	Threads  int `yaml:"threads"`
}

// Config holds all application configuration.
type Config struct {
	SourceDirectories  []string     `yaml:"source_directories"`
	BackupDirectory    string       `yaml:"backup_directory"`
	SplitSizeMB        int64        `yaml:"split_size_mb"`
	RetentionKeep      int          `yaml:"retention_keep"`
	LogLevel           string       `yaml:"log_level"`
	IODiagnostics      bool         `yaml:"io_diagnostics"`
	VerifyAfterBackup  bool         `yaml:"verify_after_backup"`
	ReminderDays       *int         `yaml:"reminder_days"`
	AuthenticationMode AuthMode     `yaml:"authentication_mode"`
	YubiKeySpare       bool         `yaml:"yubikey_spare"`
	RecoveryCode       bool         `yaml:"recovery_code"`
	PasswordMinLength  int          `yaml:"password_min_length"`
	Exclude            []string     `yaml:"exclude"`
	OnUnreadableFile   string       `yaml:"on_unreadable_file"`
	Differential       Differential `yaml:"differential"`
	Argon2             Argon2       `yaml:"argon2"`

	// ExcludeMatcher is the parsed form of Exclude, set by Load. A nil
	// matcher excludes nothing.
	ExcludeMatcher *ExcludeMatcher `yaml:"-"`

	// Argon2Notices holds human-readable notices about argon2 values that were
	// clamped to their enforced maximums during Load. Populated at load time,
	// never read from YAML, and surfaced as warnings by the startup health check.
	Argon2Notices []string `yaml:"-"`
}

// Backup reminder: the start screen warns when the newest backup is older
// than reminder_days. 0 turns the reminder off.
const (
	DefaultReminderDays = 7
	MaxReminderDays     = 365
)

// ReminderLimit returns after how many days without a backup the start
// screen warns; 0 means never.
func (c *Config) ReminderLimit() int {
	if c.ReminderDays == nil {
		return DefaultReminderDays
	}
	return *c.ReminderDays
}

// UseYubiKey reports whether the configured authentication mode requires a YubiKey.
func (c *Config) UseYubiKey() bool {
	return c.AuthenticationMode == AuthModePasswordYubiKey || c.AuthenticationMode == AuthModeYubiKey
}

// IsYubiKeyOnly reports whether authentication relies solely on the YubiKey (no password).
func (c *Config) IsYubiKeyOnly() bool {
	return c.AuthenticationMode == AuthModeYubiKey
}

// Differential holds the settings of differential backups. Zero values
// mean "use the default"; use the accessor methods.
type Differential struct {
	Enabled                    *bool `yaml:"enabled"`
	FullBackupIntervalDays     int   `yaml:"full_backup_interval_days"`
	MaxSizePercent             int   `yaml:"max_size_percent"`
	RetentionKeepDifferentials int   `yaml:"retention_keep_differentials"`
}

// Differential backup defaults and bounds.
const (
	DefaultFullBackupIntervalDays = 30
	MaxFullBackupIntervalDays     = 365
	DefaultMaxSizePercent         = 50
)

// IsEnabled reports whether differential backups are enabled (default true).
func (d Differential) IsEnabled() bool { return d.Enabled == nil || *d.Enabled }

// IntervalDays returns the maximum age of a full backup used as base.
func (d Differential) IntervalDays() int {
	if d.FullBackupIntervalDays <= 0 {
		return DefaultFullBackupIntervalDays
	}
	return d.FullBackupIntervalDays
}

// SizePercent returns the differential size, in percent of the full backup,
// at which a new full backup is created.
func (d Differential) SizePercent() int {
	if d.MaxSizePercent <= 0 {
		return DefaultMaxSizePercent
	}
	return d.MaxSizePercent
}

// on_unreadable_file values.
const (
	OnUnreadableFail = "fail"
	OnUnreadableSkip = "skip"
)

// SkipUnreadableFiles reports whether files that cannot be read are skipped
// (listed as warnings) instead of aborting the backup.
func (c *Config) SkipUnreadableFiles() bool {
	return c.OnUnreadableFile == OnUnreadableSkip
}

// DefaultSplitSizeMB is 4 GB expressed in megabytes.
const DefaultSplitSizeMB int64 = 4096

// Password length bounds (in characters) for new keys. The floor keeps a typo
// in config.yaml from allowing trivially short passwords.
const (
	DefaultPasswordMinLength = 12
	PasswordMinLengthFloor   = 8
	PasswordMinLengthMax     = 256
)

// Load reads and validates the YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Config file not found: %w\n"+
			"Remedy: Place 'config.yaml' in the same directory as the application or start RestoreSafe from that directory.", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		hint := "\nRemedy: Check YAML syntax (space indentation, correct colons, no tabs)."
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "hexdecimal number") || strings.Contains(errMsg, "hexadecimal number") {
			hint += " For Windows paths, prefer forward slashes (e.g. C:/Users/Name) or escaped backslashes inside quotes (C:\\\\Users\\\\Name)."
		}

		return nil, fmt.Errorf("Config file is invalid: %w%s", err, hint)
	}

	cfg.withDefaults()
	cfg.clampArgon2()
	return &cfg, cfg.validate()
}

// clampArgon2 lowers any argon2 value that exceeds its enforced maximum and
// records a notice for each adjustment. Maximums are clamped rather than
// rejected so an over-aggressive config still runs; values below the minimums
// remain hard errors in validate().
func (c *Config) clampArgon2() {
	if c.Argon2.Time > Argon2MaxTime {
		c.Argon2Notices = append(c.Argon2Notices, fmt.Sprintf("argon2.time %d exceeds the maximum %d; %d will be used.", c.Argon2.Time, Argon2MaxTime, Argon2MaxTime))
		c.Argon2.Time = Argon2MaxTime
	}
	if c.Argon2.MemoryMB > Argon2MaxMemoryMB {
		c.Argon2Notices = append(c.Argon2Notices, fmt.Sprintf("argon2.memory_mb %d exceeds the maximum %d; %d will be used.", c.Argon2.MemoryMB, Argon2MaxMemoryMB, Argon2MaxMemoryMB))
		c.Argon2.MemoryMB = Argon2MaxMemoryMB
	}
	if c.Argon2.Threads > Argon2MaxThreads {
		c.Argon2Notices = append(c.Argon2Notices, fmt.Sprintf("argon2.threads %d exceeds the maximum %d; %d will be used.", c.Argon2.Threads, Argon2MaxThreads, Argon2MaxThreads))
		c.Argon2.Threads = Argon2MaxThreads
	}
}

func (c *Config) withDefaults() {
	if c.SplitSizeMB <= 0 {
		c.SplitSizeMB = DefaultSplitSizeMB
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.AuthenticationMode == 0 {
		c.AuthenticationMode = AuthModePassword
	}
	if c.PasswordMinLength == 0 {
		c.PasswordMinLength = DefaultPasswordMinLength
	}
	if c.OnUnreadableFile == "" {
		c.OnUnreadableFile = OnUnreadableFail
	}
	if c.Argon2.Time == 0 {
		c.Argon2.Time = int(cryptox.DefaultArgon2Params.Time)
	}
	if c.Argon2.MemoryMB == 0 {
		c.Argon2.MemoryMB = int(cryptox.DefaultArgon2Params.MemoryKB / 1024)
	}
	if c.Argon2.Threads == 0 {
		c.Argon2.Threads = int(cryptox.DefaultArgon2Params.Threads)
	}
}

func (c *Config) validate() error {
	if len(c.SourceDirectories) == 0 {
		return fmt.Errorf("No 'source_directories' specified in config file. Remedy: Add at least one source directory under 'source_directories', e.g. ['C:/Users/Name/Documents'].")
	}
	if c.BackupDirectory == "" {
		return fmt.Errorf("No 'backup_directory' specified in config file. Remedy: Set a backup directory, e.g. 'C:/Backups'.")
	}
	switch c.LogLevel {
	case "debug", "info":
	default:
		return fmt.Errorf("Invalid 'log_level': %q (allowed: debug, info). Remedy: Set 'log_level' to 'info' or 'debug'.", c.LogLevel)
	}
	if c.RetentionKeep < 0 {
		return fmt.Errorf("Invalid 'retention_keep': %d (must be >= 0). Remedy: Use 0 (disabled) or a positive number, e.g. 7.", c.RetentionKeep)
	}
	switch c.AuthenticationMode {
	case AuthModePassword, AuthModePasswordYubiKey, AuthModeYubiKey:
	default:
		return fmt.Errorf("Invalid 'authentication_mode': %d (allowed: 1 = password only, 2 = password + YubiKey, 3 = YubiKey only). Remedy: Set 'authentication_mode' to 1, 2, or 3.", c.AuthenticationMode)
	}
	if c.YubiKeySpare && !c.UseYubiKey() {
		return fmt.Errorf("'yubikey_spare: true' requires a YubiKey mode. Remedy: Set 'authentication_mode' to 2 or 3, or set 'yubikey_spare' to false.")
	}
	switch c.OnUnreadableFile {
	case OnUnreadableFail, OnUnreadableSkip:
	default:
		return fmt.Errorf("Invalid 'on_unreadable_file': %q (allowed: fail, skip). Remedy: Set 'on_unreadable_file' to \"fail\" or \"skip\".", c.OnUnreadableFile)
	}
	if c.ReminderDays != nil && (*c.ReminderDays < 0 || *c.ReminderDays > MaxReminderDays) {
		return fmt.Errorf("Invalid 'reminder_days': %d (allowed 0-%d). Remedy: Set it to the number of days without a backup after which RestoreSafe reminds you, or 0 to turn the reminder off; the default is %d.", *c.ReminderDays, MaxReminderDays, DefaultReminderDays)
	}
	d := c.Differential
	if d.FullBackupIntervalDays < 0 || d.FullBackupIntervalDays > MaxFullBackupIntervalDays {
		return fmt.Errorf("Invalid 'differential.full_backup_interval_days': %d (allowed 1-%d). Remedy: Set it to the maximum age in days of a full backup; the default is %d.", d.FullBackupIntervalDays, MaxFullBackupIntervalDays, DefaultFullBackupIntervalDays)
	}
	if d.MaxSizePercent < 0 || d.MaxSizePercent > 100 {
		return fmt.Errorf("Invalid 'differential.max_size_percent': %d (allowed 1-100). Remedy: Set it to a percentage of the full backup size; the default is %d.", d.MaxSizePercent, DefaultMaxSizePercent)
	}
	if d.RetentionKeepDifferentials < 0 {
		return fmt.Errorf("Invalid 'differential.retention_keep_differentials': %d (must be >= 0). Remedy: Use 0 (keep all) or a positive number.", d.RetentionKeepDifferentials)
	}
	matcher, err := NewExcludeMatcher(c.Exclude)
	if err != nil {
		return err
	}
	c.ExcludeMatcher = matcher
	if c.PasswordMinLength < PasswordMinLengthFloor || c.PasswordMinLength > PasswordMinLengthMax {
		return fmt.Errorf("Invalid 'password_min_length': %d (allowed %d-%d). Remedy: Set 'password_min_length' to at least %d; the recommended value is %d.", c.PasswordMinLength, PasswordMinLengthFloor, PasswordMinLengthMax, PasswordMinLengthFloor, DefaultPasswordMinLength)
	}
	if c.Argon2.Time < Argon2MinTime {
		return fmt.Errorf("Invalid 'argon2.time': %d (minimum %d). Remedy: Set 'argon2.time' to %d or higher; the recommended value is 3.", c.Argon2.Time, Argon2MinTime, Argon2MinTime)
	}
	if c.Argon2.MemoryMB < Argon2MinMemoryMB {
		return fmt.Errorf("Invalid 'argon2.memory_mb': %d (minimum %d). Remedy: Set 'argon2.memory_mb' to %d or higher; the recommended value is 512.", c.Argon2.MemoryMB, Argon2MinMemoryMB, Argon2MinMemoryMB)
	}
	if c.Argon2.Threads < Argon2MinThreads {
		return fmt.Errorf("Invalid 'argon2.threads': %d (minimum %d). Remedy: Set 'argon2.threads' to %d or higher; the recommended value is 4.", c.Argon2.Threads, Argon2MinThreads, Argon2MinThreads)
	}
	return nil
}
