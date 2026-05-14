package util

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AuthMode represents the authentication mode for backup operations.
type AuthMode int

// AuthMode values.
const (
	AuthModePassword        AuthMode = 1 // password only
	AuthModePasswordYubiKey AuthMode = 2 // password + YubiKey HMAC-SHA1
	AuthModeYubiKey         AuthMode = 3 // YubiKey only, no password
)

// Label returns a human-readable description of the authentication mode.
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

// Argon2Config holds the Argon2id key-derivation tuning knobs exposed in config.yaml.
//
// What these parameters do:
//   - Time: number of passes over memory (more = slower to brute-force, slower to run).
//   - MemoryMB: working memory in megabytes (more = harder for GPUs, more RAM used).
//   - Threads: parallel lanes (should match physical CPU cores; beyond that, no benefit).
//
// Documented minimums (enforced by validation): Time ≥ 2, MemoryMB ≥ 64, Threads ≥ 1.
// Defaults: Time = 3, MemoryMB = 512, Threads = 4.
type Argon2Config struct {
	Time     int `yaml:"time"`
	MemoryMB int `yaml:"memory_mb"`
	Threads  int `yaml:"threads"`
}

// Config holds all application configuration.
type Config struct {
	SourceFolders      []string     `yaml:"source_folders"`
	TargetFolder       string       `yaml:"target_folder"`
	SplitSizeMB        int64        `yaml:"split_size_mb"`
	RetentionKeep      int          `yaml:"retention_keep"`
	LogLevel           string       `yaml:"log_level"`
	IODiagnostics      bool         `yaml:"io_diagnostics"`
	AuthenticationMode AuthMode     `yaml:"authentication_mode"`
	Argon2             Argon2Config `yaml:"argon2"`
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
			"Remedy: Place 'config.yaml' in the same folder as the application or start RestoreSafe from that folder.", err)
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
	return &cfg, cfg.validate()
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
		c.Argon2.Time = 3
	}
	if c.Argon2.MemoryMB == 0 {
		c.Argon2.MemoryMB = 512
	}
	if c.Argon2.Threads == 0 {
		c.Argon2.Threads = 4
	}
}

func (c *Config) validate() error {
	if len(c.SourceFolders) == 0 {
		return fmt.Errorf("No 'source_folders' specified in config file. Remedy: Add at least one source folder under 'source_folders', e.g. ['C:/Users/Name/Documents'].")
	}
	if c.TargetFolder == "" {
		return fmt.Errorf("No 'target_folder' specified in config file. Remedy: Set a target folder, e.g. 'C:/Backups'.")
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
	if c.Argon2.Time < 2 {
		return fmt.Errorf("Invalid 'argon2.time': %d (minimum 2). Remedy: Set 'argon2.time' to 2 or higher; the recommended value is 3.", c.Argon2.Time)
	}
	if c.Argon2.MemoryMB < 64 {
		return fmt.Errorf("Invalid 'argon2.memory_mb': %d (minimum 64). Remedy: Set 'argon2.memory_mb' to 64 or higher; the recommended value is 512.", c.Argon2.MemoryMB)
	}
	if c.Argon2.Threads < 1 {
		return fmt.Errorf("Invalid 'argon2.threads': %d (minimum 1). Remedy: Set 'argon2.threads' to 1 or higher; the recommended value is 4.", c.Argon2.Threads)
	}
	return nil
}
