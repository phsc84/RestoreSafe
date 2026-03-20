package startup

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthSeverityLabel(t *testing.T) {
	t.Parallel()

	if got := healthSeverityLabel(healthOK); got != "OK" {
		t.Fatalf("expected OK label, got %q", got)
	}
	if got := healthSeverityLabel(healthWarn); got != "WARN" {
		t.Fatalf("expected WARN label, got %q", got)
	}
	if got := healthSeverityLabel(healthError); got != "ERROR" {
		t.Fatalf("expected ERROR label, got %q", got)
	}
}

func TestOrphanChallengeFilesReturnsSortedList(t *testing.T) {
	t.Parallel()

	actual := map[string]bool{"b.challenge": true, "a.challenge": true, "c.challenge": true}
	expected := map[string]bool{"b.challenge": true}

	orphans := orphanChallengeFiles(actual, expected)
	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphan files, got %d", len(orphans))
	}
	if orphans[0] != "a.challenge" || orphans[1] != "c.challenge" {
		t.Fatalf("unexpected orphan order/content: %#v", orphans)
	}
}

func TestRunKey(t *testing.T) {
	t.Parallel()

	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}
	if got := runKey(entry); got != "2026-03-14|ABC123" {
		t.Fatalf("unexpected runKey: %q", got)
	}
}

func TestCollectStartupHealthItemsWarnsOnTrueIdenticalDuplicateSource(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	shared := filepath.Join(exeDir, "root-a", "Documents")
	target := filepath.Join(exeDir, "target")

	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatalf("failed to create shared source: %v", err)
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("failed to create target directory: %v", err)
	}

	cfg := &util.Config{
		SourceFolders: []string{shared, shared},
		TargetFolder:  target,
		SplitSizeMB:   64,
		LogLevel:      "info",
	}

	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	hasDuplicateWarn := false
	for _, item := range items {
		if item.Scope == "Source folder" && item.Severity == healthWarn && strings.Contains(strings.ToLower(item.Detail), "identical duplicate") {
			hasDuplicateWarn = true
			break
		}
	}

	if !hasDuplicateWarn {
		t.Fatalf("expected source-folder warning for true identical duplicate, got items: %#v", items)
	}
}

func TestCollectStartupHealthItemsSuppressesSameVolumeWarningOnLocalDrive(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "source-a")
	second := filepath.Join(exeDir, "source-b")
	target := filepath.Join(exeDir, "target")

	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	cfg := &util.Config{
		SourceFolders: []string{first, second},
		TargetFolder:  target,
		SplitSizeMB:   64,
		LogLevel:      "info",
	}

	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	hasLayoutWarn := false
	for _, item := range items {
		if item.Scope == "Source folder warning" && item.Severity == healthWarn && strings.Contains(strings.ToLower(item.Detail), "same drive/share") {
			hasLayoutWarn = true
			break
		}
	}

	if hasLayoutWarn {
		t.Fatalf("did not expect source folder warning for local same-volume source/target, got items: %#v", items)
	}
}

func TestShouldWarnSameVolumeSourceTarget(t *testing.T) {
	t.Parallel()

	if shouldWarnSameVolumeSourceTarget(`C:\source`, `C:\target`, false) {
		t.Fatal("did not expect warning for same local drive")
	}
	if !shouldWarnSameVolumeSourceTarget(`\\server\share\source`, `\\server\share\target`, false) {
		t.Fatal("expected warning for same network share")
	}
	if shouldWarnSameVolumeSourceTarget(`\\server\share\source`, `\\server\share\target`, true) {
		t.Fatal("did not expect warning for skipped source")
	}
}

func TestCollectStartupHealthItemsNoAliasCollisionForEncodedSpecialCharacters(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "Root-A", "Documents")
	second := filepath.Join(exeDir, "Root_A", "Documents")
	third := filepath.Join(exeDir, "Root A", "Documents")
	fourth := filepath.Join(exeDir, "Root.A", "Documents")
	fifth := filepath.Join(exeDir, "Root~A", "Documents")
	target := filepath.Join(exeDir, "target")

	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}
	if err := os.MkdirAll(third, 0o750); err != nil {
		t.Fatalf("failed to create third source: %v", err)
	}
	if err := os.MkdirAll(fourth, 0o750); err != nil {
		t.Fatalf("failed to create fourth source: %v", err)
	}
	if err := os.MkdirAll(fifth, 0o750); err != nil {
		t.Fatalf("failed to create fifth source: %v", err)
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("failed to create target directory: %v", err)
	}

	cfg := &util.Config{
		SourceFolders: []string{first, second, third, fourth, fifth},
		TargetFolder:  target,
		SplitSizeMB:   64,
		LogLevel:      "info",
	}

	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	hasCollisionError := false
	hasSourceFolderError := false
	for _, item := range items {
		if item.Scope == "Source folder" && item.Severity == healthError && strings.Contains(strings.ToLower(item.Detail), "alias collision") {
			hasCollisionError = true
		}
		if item.Scope == "Source folder" && item.Severity == healthError {
			hasSourceFolderError = true
		}
	}

	if hasCollisionError {
		t.Fatalf("did not expect alias-collision error, got items: %#v", items)
	}
	if hasSourceFolderError {
		t.Fatalf("did not expect source-folder errors for encoded special-character variants, got items: %#v", items)
	}
}

func TestPrintStartupHealthCheckSummaryAndAdvice(t *testing.T) {
	items := []healthItem{
		{Severity: healthOK, Scope: "Config", Detail: "ok"},
		{Severity: healthWarn, Scope: "Target", Detail: "warn"},
		{Severity: healthError, Scope: "Source", Detail: "error"},
	}

	output := testutil.CaptureStdout(t, func() {
		printStartupHealthCheck(items)
	})

	if !strings.Contains(output, "Summary: 1 OK, 1 warning(s), 1 error(s)") {
		t.Fatalf("summary line missing or incorrect in output: %q", output)
	}
	if !strings.Contains(output, "Review the reported errors") {
		t.Fatalf("expected advice line for errors, got output: %q", output)
	}
}
