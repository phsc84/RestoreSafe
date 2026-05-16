package startup

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthSeverityLabelDefaultReturnsUnknown(t *testing.T) {
	t.Parallel()
	if got := healthSeverityLabel(healthSeverity(99)); got != "UNKNOWN" {
		t.Fatalf("expected UNKNOWN for unknown severity, got %q", got)
	}
}

func TestCheckConfigFileHealthOKForExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	items := checkConfigFileHealth(filepath.ToSlash(configPath))
	if len(items) != 1 || items[0].Severity != healthOK {
		t.Fatalf("expected OK health item for existing config, got: %#v", items)
	}
}

func TestCheckConfigFileHealthErrorForMissingFile(t *testing.T) {
	t.Parallel()
	items := checkConfigFileHealth("/nonexistent/config.yaml")
	if len(items) != 1 || items[0].Severity != healthError {
		t.Fatalf("expected ERROR health item for missing config, got: %#v", items)
	}
}

func TestCheckBackupInventoryHealthWarnsWhenNoBackups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	items := checkBackupInventoryHealth(dir)
	if len(items) == 0 {
		t.Fatal("expected at least one health item for empty inventory")
	}
	if items[0].Severity != healthWarn {
		t.Fatalf("expected WARN for empty backup inventory, got severity: %v", items[0].Severity)
	}
}

func TestCheckTempDirHealthReturnsOK(t *testing.T) {
	t.Parallel()
	items := checkTempDirHealth()
	if len(items) == 0 {
		t.Fatal("expected at least one health item from temp dir check")
	}
	if items[0].Severity != healthOK {
		t.Fatalf("expected temp dir health to be OK, got severity %v with detail: %s", items[0].Severity, items[0].Detail)
	}
}

func TestListChallengeFilesReturnsOnlyChallengeFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"run1.challenge", "run2.challenge"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "backup.enc"), []byte("z"), 0o600); err != nil {
		t.Fatalf("failed to write enc file: %v", err)
	}

	files, err := listChallengeFiles(dir)
	if err != nil {
		t.Fatalf("listChallengeFiles returned error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 challenge files, got %d", len(files))
	}
	if !files["run1.challenge"] || !files["run2.challenge"] {
		t.Fatalf("expected both challenge files in result, got: %v", files)
	}
}

func TestListChallengeFilesEmptyDirectoryReturnsEmpty(t *testing.T) {
	t.Parallel()
	files, err := listChallengeFiles(t.TempDir())
	if err != nil {
		t.Fatalf("listChallengeFiles returned error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected empty result for empty directory, got: %v", files)
	}
}

func TestOrphanChallengeFilesReturnsEmptyWhenAllExpected(t *testing.T) {
	t.Parallel()
	actual := map[string]bool{"a.challenge": true, "b.challenge": true}
	expected := map[string]bool{"a.challenge": true, "b.challenge": true}
	if orphans := orphanChallengeFiles(actual, expected); len(orphans) != 0 {
		t.Fatalf("expected no orphans when all files are expected, got: %v", orphans)
	}
}

func TestBuildBackupInventoryIssueItemsDetectsOrphanChallengeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := util.BackupEntry{FolderName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	part := util.PartFileName(dir, entry.FolderName, entry.Date, entry.ID, 1)
	if err := os.MkdirAll(filepath.Dir(part), 0o750); err != nil {
		t.Fatalf("failed to create part dir: %v", err)
	}
	if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write part file: %v", err)
	}

	orphanChallenge := filepath.Join(dir, "[Other]_2025-01-01_XYZ999.challenge")
	if err := os.WriteFile(orphanChallenge, []byte("hex"), 0o600); err != nil {
		t.Fatalf("failed to write orphan challenge: %v", err)
	}

	items := buildBackupInventoryIssueItems(dir, []util.BackupEntry{entry})

	hasOrphanWarn := false
	for _, item := range items {
		if item.Severity == healthWarn && item.Scope == healthScopeChallengeFile {
			hasOrphanWarn = true
			break
		}
	}
	if !hasOrphanWarn {
		t.Fatalf("expected orphan challenge file warning, got items: %#v", items)
	}
}

func TestPrintStartupHealthCheckShowsTempDirItemsWithNote(t *testing.T) {
	t.Parallel()
	items := []healthItem{
		{isNote: true, Detail: "Local staging enabled."},
		{Severity: healthOK, Scope: healthScopeTempDirectory, Detail: "C:/Temp"},
	}

	var sb strings.Builder
	printStartupHealthCheck(&sb, items)
	output := sb.String()

	if !strings.Contains(output, "Local staging enabled.") {
		t.Fatalf("expected note text in output, got: %q", output)
	}
	if !strings.Contains(output, "Temp directory:") {
		t.Fatalf("expected Temp directory section in output, got: %q", output)
	}
	if !strings.Contains(output, "  [OK] C:/Temp") {
		t.Fatalf("expected temp dir OK line in output, got: %q", output)
	}
}

func TestPrintStartupHealthCheckNoAdviceLineWhenNoErrors(t *testing.T) {
	t.Parallel()
	items := []healthItem{
		{Severity: healthOK, Scope: "Config", Detail: "ok"},
		{Severity: healthWarn, Scope: "Target", Detail: "warn"},
	}

	var sb strings.Builder
	printStartupHealthCheck(&sb, items)
	output := sb.String()

	if strings.Contains(output, "Review the reported errors") {
		t.Fatalf("did not expect advice line when no errors, got: %q", output)
	}
	if !strings.Contains(output, "Summary: 1 OK, 1 warning(s), 0 error(s)") {
		t.Fatalf("expected summary line, got: %q", output)
	}
}

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
	if got := entry.RunKey(); got != "2026-03-14|ABC123" {
		t.Fatalf("unexpected RunKey: %q", got)
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
		if item.Scope == "Source folder(s)" && item.Severity == healthWarn && strings.Contains(strings.ToLower(item.Detail), "identical duplicate") {
			hasDuplicateWarn = true
			break
		}
	}

	if !hasDuplicateWarn {
		t.Fatalf("expected source-folder warning for true identical duplicate, got items: %#v", items)
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
	t.Parallel()
	items := []healthItem{
		{Severity: healthOK, Scope: "Config", Detail: "ok"},
		{Severity: healthWarn, Scope: "Target", Detail: "warn"},
		{Severity: healthError, Scope: "Source", Detail: "error"},
	}

	var sb strings.Builder
	printStartupHealthCheck(&sb, items)
	output := sb.String()

	if !strings.Contains(output, "Summary: 1 OK, 1 warning(s), 1 error(s)") {
		t.Fatalf("summary line missing or incorrect in output: %q", output)
	}
	if !strings.Contains(output, "Review the reported errors") {
		t.Fatalf("expected advice line for errors, got output: %q", output)
	}
}

func TestPrintStartupHealthCheckGroupsScopes(t *testing.T) {
	t.Parallel()

	items := []healthItem{
		{Severity: healthOK, Scope: "Source folder(s)", Detail: "C:/A"},
		{Severity: healthWarn, Scope: "Source folder(s)", Detail: "C:/B → some warning"},
		{Severity: healthOK, Scope: "Backup folder", Detail: "C:/Backup"},
	}

	var sb strings.Builder
	printStartupHealthCheck(&sb, items)
	output := sb.String()

	if strings.Count(output, "Source folder(s):") != 1 {
		t.Fatalf("expected Source folder(s) title once, got output: %q", output)
	}
	if strings.Contains(output, "[OK] Source folder(s):") {
		t.Fatalf("did not expect old inline scope format, got output: %q", output)
	}
	if !strings.Contains(output, "  [OK] C:/A") {
		t.Fatalf("expected grouped detail line for Source folder, got output: %q", output)
	}
	if !strings.Contains(output, "Backup folder:") {
		t.Fatalf("expected Backup folder title, got output: %q", output)
	}
}
