package gui

import (
	"RestoreSafe/internal/startup"
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeStateWhileChecking(t *testing.T) {
	t.Parallel()
	s := homeState{checking: true}
	if b, rv := s.actionsEnabled(); b || rv {
		t.Fatal("actions must be disabled while the health check runs")
	}
	if !strings.Contains(s.statusLine(), "Running the startup health check") {
		t.Fatalf("unexpected status %q", s.statusLine())
	}
}

func TestHomeStateBlockedByMissingSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &util.Config{SourceDirectories: []string{filepath.Join(dir, "missing")}, BackupDirectory: dir, LogLevel: "info"}
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	health := startup.CheckHealth(cfg, dir, configPath)
	s := homeState{health: &health}
	backup, restoreOrVerify := s.actionsEnabled()
	if backup || !restoreOrVerify {
		t.Fatalf("a missing source must block only the backup: backup=%v restore/verify=%v", backup, restoreOrVerify)
	}
	if !strings.HasPrefix(s.statusLine(), "Backup is blocked") {
		t.Fatalf("unexpected status %q", s.statusLine())
	}
}
