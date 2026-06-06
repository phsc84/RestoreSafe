package util

import (
	"fmt"
	"os"
	"strings"

	"RestoreSafe/internal/security"

	"gopkg.in/yaml.v3"
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
// Memory is converted from the security package's KiB to the MB unit used in
// config.yaml.
//
// Values below the minimums are rejected by validate(). Values above the
// maximums are clamped to the maximum (with a startup warning) rather than
// rejected, so an over-aggressive config still runs and stays restorable. The
// threads maximum also keeps the value within the uint8 range Argon2 requires,
// preventing a silent overflow when the value is cast for key derivation.
const (
	Argon2MinTime     = security.MinArgonTime
	Argon2MinMemoryMB = security.MinArgonMemoryKB / 1024
	Argon2MinThreads  = security.MinArgonThreads

	Argon2MaxTime     = security.MaxArgonTime
	Argon2MaxMemoryMB = security.MaxArgonMemoryKB / 1024
	Argon2MaxThreads  = security.MaxArgonThreads
)

// Argon2Config holds the Argon2id key-derivation tuning knobs exposed in config.yaml.
//
// What these parameters do:
//   - Time: number of passes over memory (more = slower to brute-force, slower to run).
//   - MemoryMB: working memory in megabytes (more = harder for GPUs, more RAM used).
//   - Threads: parallel lanes (should match physical CPU cores; beyond that, no benefit).
//
// Minimums (enforced by validation): Time ≥ 2, MemoryMB ≥ 64, Threads ≥ 1.
// Maximums (clamped with a warning): Time ≤ 20, MemoryMB ≤ 4096, Threads ≤ 255.
// Defaults: Time = 3, MemoryMB = 512, Threads = 4.
type Argon2Config struct {
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
	AuthenticationMode AuthMode     `yaml:"authentication_mode"`
	Argon2             Argon2Config `yaml:"argon2"`

	// Argon2Notices holds human-readable notices about argon2 values that were
	// clamped to their enforced maximums during Load. Populated at load time,
	// never read from YAML, and surfaced as warnings by the startup health check.
	Argon2Notices []string `yaml:"-"`
}

// UseYubiKey reports whether the configured authentication mode requires a YubiKey.
func (c *Config) UseYubiKey() bool {
	return c.AuthenticationMode == AuthModePasswordYubiKey || c.AuthenticationMode == AuthModeYubiKey
}

// IsYubiKeyOnly reports whether authentication relies solely on the YubiKey (no password).
func (c *Config) IsYubiKeyOnly() bool {
	return c.AuthenticationMode == AuthModeYubiKey
}

// DefaultSplitSizeMB is 4 GB expressed in megabytes.
const DefaultSplitSizeMB int64 = 4096

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
	if c.Argon2.Time == 0 {
		c.Argon2.Time = int(security.DefaultArgon2Params.Time)
	}
	if c.Argon2.MemoryMB == 0 {
		c.Argon2.MemoryMB = int(security.DefaultArgon2Params.MemoryKB / 1024)
	}
	if c.Argon2.Threads == 0 {
		c.Argon2.Threads = int(security.DefaultArgon2Params.Threads)
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
