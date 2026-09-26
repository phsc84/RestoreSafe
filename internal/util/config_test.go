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
backup_directory: "C:/Backup"
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

func loadConfigText(t *testing.T, extra string) (*Config, error) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "source_directories:\n  - \"C:/Users/Test/Documents\"\nbackup_directory: \"C:/Backup\"\n" + extra
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return Load(cfgPath)
}

func TestLoadKeyOptions(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigText(t, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PasswordMinLength != DefaultPasswordMinLength || cfg.YubiKeySpare || cfg.RecoveryCode {
		t.Fatalf("unexpected defaults: min=%d spare=%v recovery=%v", cfg.PasswordMinLength, cfg.YubiKeySpare, cfg.RecoveryCode)
	}

	cfg, err = loadConfigText(t, "authentication_mode: 2\nyubikey_spare: true\nrecovery_code: true\npassword_min_length: 8\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.YubiKeySpare || !cfg.RecoveryCode || cfg.PasswordMinLength != 8 {
		t.Fatalf("options not parsed: %+v", cfg)
	}

	for _, tc := range []struct{ extra, want string }{
		{"password_min_length: 7\n", "password_min_length"},
		{"password_min_length: 257\n", "password_min_length"},
		{"authentication_mode: 1\nyubikey_spare: true\n", "yubikey_spare"},
	} {
		if _, err := loadConfigText(t, tc.extra); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: expected error about %s, got %v", tc.extra, tc.want, err)
		}
	}
}

func TestLoadBackupOptions(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigText(t, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SkipUnreadableFiles() || cfg.OnUnreadableFile != OnUnreadableFail || !cfg.ExcludeMatcher.Empty() {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	cfg, err = loadConfigText(t, "on_unreadable_file: skip\nexclude:\n  - \"*.tmp\"\n  - \"/Cache\"\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.SkipUnreadableFiles() || !cfg.ExcludeMatcher.Match("x/a.tmp", false) || !cfg.ExcludeMatcher.Match("Cache", true) {
		t.Fatalf("options not applied: %+v", cfg)
	}

	for _, tc := range []struct{ extra, want string }{
		{"on_unreadable_file: ignore\n", "on_unreadable_file"},
		{"exclude:\n  - \"[abc\"\n", "Invalid exclude pattern"},
	} {
		if _, err := loadConfigText(t, tc.extra); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: expected error about %s, got %v", tc.extra, tc.want, err)
		}
	}
}

func TestLoadDifferentialOptions(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigText(t, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := cfg.Differential
	if !d.IsEnabled() || d.IntervalDays() != 30 || d.SizePercent() != 50 || d.RetentionKeepDifferentials != 0 {
		t.Fatalf("unexpected defaults: %+v", d)
	}

	cfg, err = loadConfigText(t, "differential:\n  enabled: false\n  full_backup_interval_days: 7\n  max_size_percent: 25\n  retention_keep_differentials: 5\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d = cfg.Differential
	if d.IsEnabled() || d.IntervalDays() != 7 || d.SizePercent() != 25 || d.RetentionKeepDifferentials != 5 {
		t.Fatalf("options not parsed: %+v", d)
	}

	for _, extra := range []string{
		"differential:\n  full_backup_interval_days: 366\n",
		"differential:\n  max_size_percent: 101\n",
		"differential:\n  retention_keep_differentials: -1\n",
	} {
		if _, err := loadConfigText(t, extra); err == nil || !strings.Contains(err.Error(), "differential.") {
			t.Fatalf("%q: expected differential error, got %v", extra, err)
		}
	}
}

func TestLoadRejectsNegativeRetentionKeep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
backup_directory: "C:/Backup"
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
backup_directory: "C:/Backup"
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
backup_directory: "C:/Backup"
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
backup_directory: "C:/Backup"
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
	if err := os.WriteFile(cfgPath, []byte("backup_directory: \"C:/Backup\"\n"), 0o600); err != nil {
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

func TestLoadRejectsMissingBackupDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("source_directories:\n  - \"C:/Docs\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing backup_directory, got nil")
	}
	if !strings.Contains(err.Error(), "backup_directory") {
		t.Fatalf("expected backup_directory error, got: %v", err)
	}
}

func TestLoadDefaultsArgon2Params(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Users/Test/Documents"
backup_directory: "C:/Backup"
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
			content := fmt.Sprintf("source_directories:\n  - \"C:/Docs\"\nbackup_directory: \"C:/Backup\"\n%s", tc.yaml)
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

func TestLoadClampsArgon2AboveMaximums(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `source_directories:
  - "C:/Docs"
backup_directory: "C:/Backup"
argon2:
  time: 99
  memory_mb: 99999
  threads: 9999
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Argon2.Time != Argon2MaxTime {
		t.Errorf("expected argon2.time clamped to %d, got %d", Argon2MaxTime, cfg.Argon2.Time)
	}
	if cfg.Argon2.MemoryMB != Argon2MaxMemoryMB {
		t.Errorf("expected argon2.memory_mb clamped to %d, got %d", Argon2MaxMemoryMB, cfg.Argon2.MemoryMB)
	}
	if cfg.Argon2.Threads != Argon2MaxThreads {
		t.Errorf("expected argon2.threads clamped to %d, got %d", Argon2MaxThreads, cfg.Argon2.Threads)
	}
	if len(cfg.Argon2Notices) != 3 {
		t.Fatalf("expected 3 clamp notices, got %d: %v", len(cfg.Argon2Notices), cfg.Argon2Notices)
	}
	for _, notice := range cfg.Argon2Notices {
		if !strings.Contains(notice, "exceeds the maximum") {
			t.Errorf("expected clamp notice to mention the maximum, got: %q", notice)
		}
	}
}

func TestLoadAcceptsArgon2AtMaximumsWithoutNotice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := fmt.Sprintf(`source_directories:
  - "C:/Docs"
backup_directory: "C:/Backup"
argon2:
  time: %d
  memory_mb: %d
  threads: %d
`, Argon2MaxTime, Argon2MaxMemoryMB, Argon2MaxThreads)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Argon2Notices) != 0 {
		t.Fatalf("expected no clamp notices at the maximums, got: %v", cfg.Argon2Notices)
	}
}

func TestAuthModeLabel(t *testing.T) {
	t.Parallel()
	cases := map[AuthMode]string{
		AuthModePassword:        "password only",
		AuthModePasswordYubiKey: "password + YubiKey",
		AuthModeYubiKey:         "YubiKey only (no password)",
	}
	for mode, want := range cases {
		if got := mode.Label(); got != want {
			t.Errorf("AuthMode(%d).Label() = %q, want %q", mode, got, want)
		}
	}
}

func TestAuthModeFromFactors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		usesYubiKey bool
		noPassword  bool
		want        AuthMode
	}{
		{false, false, AuthModePassword},
		{false, true, AuthModePassword}, // noPassword is ignored when no YubiKey is used
		{true, false, AuthModePasswordYubiKey},
		{true, true, AuthModeYubiKey},
	}
	for _, tc := range cases {
		if got := AuthModeFromFactors(tc.usesYubiKey, tc.noPassword); got != tc.want {
			t.Errorf("AuthModeFromFactors(%v, %v) = %d, want %d", tc.usesYubiKey, tc.noPassword, got, tc.want)
		}
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
