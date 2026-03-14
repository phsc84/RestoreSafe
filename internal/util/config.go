package util

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	SourceFolders []string `yaml:"source_folders"`
	TargetFolder  string   `yaml:"target_folder"`
	SplitSizeMB   int64    `yaml:"split_size_mb"`
	RetentionKeep int      `yaml:"retention_keep"`
	LogLevel      string   `yaml:"log_level"`
	IODiagnostics bool     `yaml:"io_diagnostics"`
	YubikeyEnable bool     `yaml:"yubikey_enable"`
}

// DefaultSplitSizeMB is 4 GB expressed in megabytes.
const DefaultSplitSizeMB int64 = 4096

// Load reads and validates the YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Config file not found: %w\n"+
			"Make sure 'config.yaml' is in the same directory as the application.", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		hint := ""
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "hexdecimal number") || strings.Contains(errMsg, "hexadecimal number") {
			hint = "\nHint: Windows paths in YAML need escaping in double quotes (e.g. C:\\\\Users\\\\Name) or use forward slashes (C:/Users/Name)."
		}

		return nil, fmt.Errorf("Config file is invalid: %w%s", err, hint)
	}

	return cfg.withDefaults(), cfg.validate()
}

func (c *Config) withDefaults() *Config {
	if c.SplitSizeMB <= 0 {
		c.SplitSizeMB = DefaultSplitSizeMB
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	return c
}

func (c *Config) validate() error {
	if len(c.SourceFolders) == 0 {
		return fmt.Errorf("No 'source_folders' specified in config file.")
	}
	if c.TargetFolder == "" {
		return fmt.Errorf("No 'target_folder' specified in config file.")
	}
	switch c.LogLevel {
	case "debug", "info":
	default:
		return fmt.Errorf("Invalid 'log_level': %q (allowed: debug, info)", c.LogLevel)
	}
	if c.RetentionKeep < 0 {
		return fmt.Errorf("Invalid 'retention_keep': %d (must be >= 0)", c.RetentionKeep)
	}
	return nil
}
