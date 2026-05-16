package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppliesDefaultsAndParsesRetention(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
target_directory: "C:/Backup"
retention_keep: 3
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.SplitSizeMB != DefaultSplitSizeMB {
		t.Fatalf("expected default split size %d, got %d", DefaultSplitSizeMB, cfg.SplitSizeMB)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected default log level info, got %q", cfg.LogLevel)
	}
	if cfg.RetentionKeep != 3 {
		t.Fatalf("expected retention_keep 3, got %d", cfg.RetentionKeep)
	}
}

func TestLoadRejectsNegativeRetentionKeep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
target_directory: "C:/Backup"
retention_keep: -1
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for negative retention_keep, got nil")
	}
	if !strings.Contains(err.Error(), "retention_keep") {
		t.Fatalf("expected retention_keep error, got: %v", err)
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
target_directory: "C:/Backup"
log_level: "trace"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid log_level, got nil")
	}
	if !strings.Contains(err.Error(), "log_level") {
		t.Fatalf("expected log_level error, got: %v", err)
	}
}

func TestLoadDefaultsAuthenticationMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
target_directory: "C:/Backup"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AuthenticationMode != AuthModePassword {
		t.Fatalf("expected default authentication_mode %d, got %d", AuthModePassword, cfg.AuthenticationMode)
	}
}

func TestLoadRejectsInvalidAuthenticationMode(t *testing.T) {
	t.Parallel()

	for _, bad := range []int{5, -1, 99} {
		bad := bad
		t.Run(fmt.Sprintf("mode_%d", bad), func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfgContent := fmt.Sprintf(`source_directories:
  - "C:/Users/Test/Documents"
target_directory: "C:/Backup"
authentication_mode: %d
`, bad)
			if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			_, err := Load(cfgPath)
			if err == nil {
				t.Fatalf("expected error for authentication_mode %d, got nil", bad)
			}
			if !strings.Contains(err.Error(), "authentication_mode") {
				t.Fatalf("expected authentication_mode error, got: %v", err)
			}
		})
	}
}

func TestLoadRejectsMissingSourceDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("target_directory: \"C:/Backup\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing source_directories, got nil")
	}
	if !strings.Contains(err.Error(), "source_directories") {
		t.Fatalf("expected source_directories error, got: %v", err)
	}
}

func TestLoadRejectsMissingTargetDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("source_directories:\n  - \"C:/Docs\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing target_directory, got nil")
	}
	if !strings.Contains(err.Error(), "target_directory") {
		t.Fatalf("expected target_directory error, got: %v", err)
	}
}

func TestLoadDefaultsArgon2Params(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
target_directory: "C:/Backup"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Argon2.Time != 3 {
		t.Errorf("expected default argon2.time 3, got %d", cfg.Argon2.Time)
	}
	if cfg.Argon2.MemoryMB != 512 {
		t.Errorf("expected default argon2.memory_mb 512, got %d", cfg.Argon2.MemoryMB)
	}
	if cfg.Argon2.Threads != 4 {
		t.Errorf("expected default argon2.threads 4, got %d", cfg.Argon2.Threads)
	}
}

func TestLoadRejectsInvalidArgon2Params(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantKey string
	}{
		{
			name:    "time_negative",
			yaml:    "argon2:\n  time: -1\n  memory_mb: 64\n  threads: 4\n",
			wantKey: "argon2.time",
		},
		{
			name:    "time_below_minimum",
			yaml:    "argon2:\n  time: 1\n  memory_mb: 64\n  threads: 4\n",
			wantKey: "argon2.time",
		},
		{
			name:    "memory_too_low",
			yaml:    "argon2:\n  time: 3\n  memory_mb: 4\n  threads: 4\n",
			wantKey: "argon2.memory_mb",
		},
		{
			name:    "memory_below_minimum",
			yaml:    "argon2:\n  time: 3\n  memory_mb: 32\n  threads: 4\n",
			wantKey: "argon2.memory_mb",
		},
		{
			name:    "threads_negative",
			yaml:    "argon2:\n  time: 3\n  memory_mb: 64\n  threads: -1\n",
			wantKey: "argon2.threads",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			content := fmt.Sprintf("source_directories:\n  - \"C:/Docs\"\ntarget_directory: \"C:/Backup\"\n%s", tc.yaml)
			if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			_, err := Load(cfgPath)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("expected %q in error, got: %v", tc.wantKey, err)
			}
		})
	}
}

func TestConfigUseYubiKeyAndIsYubiKeyOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode        AuthMode
		useYubiKey  bool
		yubiKeyOnly bool
	}{
		{AuthModePassword, false, false},
		{AuthModePasswordYubiKey, true, false},
		{AuthModeYubiKey, true, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("mode_%d", tc.mode), func(t *testing.T) {
			t.Parallel()
			cfg := &Config{AuthenticationMode: tc.mode}
			if got := cfg.UseYubiKey(); got != tc.useYubiKey {
				t.Errorf("UseYubiKey() = %v, want %v", got, tc.useYubiKey)
			}
			if got := cfg.IsYubiKeyOnly(); got != tc.yubiKeyOnly {
				t.Errorf("IsYubiKeyOnly() = %v, want %v", got, tc.yubiKeyOnly)
			}
		})
	}
}
