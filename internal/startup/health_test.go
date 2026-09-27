package startup

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/ui"
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

func passwordConfig() *util.Config {
	return &util.Config{AuthenticationMode: util.AuthModePassword}
}

func findItem(items []healthItem, severity healthSeverity, scope, contains string) bool {
	for _, item := range items {
		if item.Severity == severity && item.Scope == scope && strings.Contains(item.Detail, contains) {
			return true
		}
	}
	return false
}

func TestCheckBackupInventoryHealthWarnsWhenNoBackups(t *testing.T) {
	t.Parallel()
	items := checkBackupInventoryHealth(passwordConfig(), t.TempDir())
	if !findItem(items, healthWarn, healthScopeBackupInventory, "No backup sets found") {
		t.Fatalf("expected WARN for empty backup inventory, got: %#v", items)
	}
	if !findItem(items, healthOK, healthScopeKeys, "next backup creates new keys") {
		t.Fatalf("expected key status for empty inventory, got: %#v", items)
	}
}

func TestCheckBackupInventoryHealthReportsCompleteSetAndKeys(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	items := checkBackupInventoryHealth(passwordConfig(), fx.BackupDir)
	if !findItem(items, healthOK, healthScopeBackupInventory, "structurally complete") {
		t.Fatalf("expected complete inventory, got: %#v", items)
	}
	if !findItem(items, healthOK, healthScopeKeys, "Current keys created") {
		t.Fatalf("expected key summary, got: %#v", items)
	}

	yubiCfg := &util.Config{AuthenticationMode: util.AuthModePasswordYubiKey}
	if items := checkBackupInventoryHealth(yubiCfg, fx.BackupDir); !findItem(items, healthWarn, healthScopeKeys, "creates new keys") {
		t.Fatalf("expected warning about new keys after mode change, got: %#v", items)
	}
}

func TestCheckBackupInventoryHealthReportsIncompleteAndOrphanSets(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("pw"))
	// A differential part whose full backup does not exist.
	orphan := util.BackupEntry{DirectoryName: "Other", ChainID: "ORP001", Date: "2026-03-14", DiffNumber: 1}
	if err := os.WriteFile(util.PartFileName(fx.BackupDir, orphan, 1), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	items := checkBackupInventoryHealth(passwordConfig(), fx.BackupDir)
	if !findItem(items, healthError, healthScopeBackupSet, orphan.String()) {
		t.Fatalf("expected error for broken set, got: %#v", items)
	}
	if findItem(items, healthOK, healthScopeBackupInventory, "structurally complete") {
		t.Fatalf("did not expect complete inventory message, got: %#v", items)
	}
}

func TestCheckBackupInventoryHealthWarnsAboutLegacyAndTempFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"[Docs]_2026-01-01_OLD001-001.enc", "[Docs]_ABC123_2026-03-14_FULL-001.enc.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	items := checkBackupInventoryHealth(passwordConfig(), dir)
	if !findItem(items, healthWarn, healthScopeBackupInventory, "RestoreSafe 1.x backups found") {
		t.Fatalf("expected 1.x warning, got: %#v", items)
	}
	if !findItem(items, healthWarn, healthScopeBackupInventory, "interrupted backup") {
		t.Fatalf("expected leftover warning, got: %#v", items)
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

func TestCheckArgon2HealthWarnsOnClampNotices(t *testing.T) {
	t.Parallel()

	cfg := &util.Config{
		Argon2Notices: []string{
			"argon2.memory_mb 8192 exceeds the maximum 4096; 4096 will be used.",
		},
	}

	items := checkArgon2Health(cfg)
	if len(items) != 1 {
		t.Fatalf("expected 1 argon2 health item, got %d: %#v", len(items), items)
	}
	if items[0].Severity != healthWarn {
		t.Fatalf("expected WARN severity, got %v", items[0].Severity)
	}
	if items[0].Scope != healthScopeArgon2 {
		t.Fatalf("expected scope %q, got %q", healthScopeArgon2, items[0].Scope)
	}
	if !strings.Contains(items[0].Detail, "exceeds the maximum") {
		t.Fatalf("expected detail to mention the maximum, got: %q", items[0].Detail)
	}
}

func TestCheckArgon2HealthSilentWithoutNotices(t *testing.T) {
	t.Parallel()

	if items := checkArgon2Health(&util.Config{}); len(items) != 0 {
		t.Fatalf("expected no argon2 health items without notices, got: %#v", items)
	}
}

func TestPrintStartupHealthCheckShowsTempDirItemsWithNote(t *testing.T) {
	t.Parallel()
	items := []healthItem{
		{isNote: true, Detail: "Local staging via temp directory enabled."},
		{Severity: healthOK, Scope: healthScopeTempDirectory, Detail: "C:/Temp"},
	}

	var sb strings.Builder
	printStartupHealthCheck(&sb, items)
	output := sb.String()

	if !strings.Contains(output, "Local staging via temp directory enabled.") {
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

func TestRunStartupHealthCheckPrintsReportAndReturnsResult(t *testing.T) {
	exeDir := t.TempDir()
	source := filepath.Join(exeDir, "Documents")
	backup := filepath.Join(exeDir, "Backups")
	if err := os.MkdirAll(source, 0o750); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}
	if err := os.MkdirAll(backup, 0o750); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}
	configPath := filepath.Join(exeDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg := &util.Config{
		SourceDirectories: []string{source},
		BackupDirectory:   backup,
		SplitSizeMB:       64,
		LogLevel:          "info",
	}

	var result HealthCheckResult
	output := testutil.CaptureStdout(t, func() {
		result = RunStartupHealthCheck(os.Stdout, cfg, exeDir, configPath)
	})

	if !strings.Contains(output, "Startup health check") {
		t.Fatalf("expected health check header in output, got: %q", output)
	}
	if !strings.Contains(output, "Summary:") {
		t.Fatalf("expected summary line in output, got: %q", output)
	}
	// A healthy config with YubiKey disabled should not block any operation.
	if result.BlocksBackup() {
		t.Fatalf("did not expect a healthy config to block backup, output: %q", output)
	}
	if result.BlocksRestoreOrVerify() {
		t.Fatalf("did not expect a healthy config to block restore/verify, output: %q", output)
	}
}

func TestBuildHealthCheckResultRecordsOnlyErrorScopes(t *testing.T) {
	t.Parallel()

	items := []healthItem{
		{Severity: healthOK, Scope: healthScopeConfig},
		{Severity: healthWarn, Scope: healthScopeSourceDirectory},
		{Severity: healthError, Scope: healthScopeBackupDirectory},
		{Severity: healthError, Scope: healthScopeYubiKey, isNote: true}, // notes excluded
	}

	result := buildHealthCheckResult(items)
	if !result.errorScopes[healthScopeBackupDirectory] {
		t.Fatal("expected backup directory error scope to be recorded")
	}
	if result.errorScopes[healthScopeConfig] {
		t.Fatal("did not expect OK scope to be recorded as error")
	}
	if result.errorScopes[healthScopeSourceDirectory] {
		t.Fatal("did not expect WARN scope to be recorded as error")
	}
	if result.errorScopes[healthScopeYubiKey] {
		t.Fatal("did not expect note item to be recorded as error")
	}
}

func TestBlocksBackup(t *testing.T) {
	t.Parallel()

	blockingScopes := []string{
		healthScopeConfig,
		healthScopeSourceDirectory,
		healthScopeBackupDirectory,
		healthScopeYubiKey,
		healthScopeTempDirectory,
	}
	for _, scope := range blockingScopes {
		result := HealthCheckResult{errorScopes: map[string]bool{scope: true}}
		if !result.BlocksBackup() {
			t.Fatalf("expected error in scope %q to block backup", scope)
		}
	}

	if (HealthCheckResult{}).BlocksBackup() {
		t.Fatal("expected no error scopes to allow backup")
	}
	// Backup inventory errors do not block a backup.
	nonBlocking := HealthCheckResult{errorScopes: map[string]bool{healthScopeBackupInventory: true}}
	if nonBlocking.BlocksBackup() {
		t.Fatal("did not expect backup inventory error to block backup")
	}
}

func TestBlocksRestoreOrVerify(t *testing.T) {
	t.Parallel()

	blockingScopes := []string{
		healthScopeConfig,
		healthScopeBackupDirectory,
		healthScopeYubiKey,
	}
	for _, scope := range blockingScopes {
		result := HealthCheckResult{errorScopes: map[string]bool{scope: true}}
		if !result.BlocksRestoreOrVerify() {
			t.Fatalf("expected error in scope %q to block restore/verify", scope)
		}
	}

	if (HealthCheckResult{}).BlocksRestoreOrVerify() {
		t.Fatal("expected no error scopes to allow restore/verify")
	}
	// Source and temp directory errors are irrelevant to restore/verify.
	for _, scope := range []string{healthScopeSourceDirectory, healthScopeTempDirectory} {
		result := HealthCheckResult{errorScopes: map[string]bool{scope: true}}
		if result.BlocksRestoreOrVerify() {
			t.Fatalf("did not expect error in scope %q to block restore/verify", scope)
		}
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
		t.Fatalf("failed to create backup directory: %v", err)
	}

	cfg := &util.Config{
		SourceDirectories: []string{shared, shared},
		BackupDirectory:   target,
		SplitSizeMB:       64,
		LogLevel:          "info",
	}

	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	hasDuplicateWarn := false
	for _, item := range items {
		if item.Scope == "Source directory(s)" && item.Severity == healthWarn && strings.Contains(strings.ToLower(item.Detail), "identical duplicate") {
			hasDuplicateWarn = true
			break
		}
	}

	if !hasDuplicateWarn {
		t.Fatalf("expected source-directory warning for true identical duplicate, got items: %#v", items)
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
		t.Fatalf("failed to create backup directory: %v", err)
	}

	cfg := &util.Config{
		SourceDirectories: []string{first, second, third, fourth, fifth},
		BackupDirectory:   target,
		SplitSizeMB:       64,
		LogLevel:          "info",
	}

	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	hasCollisionError := false
	hasSourceDirectoryError := false
	for _, item := range items {
		if item.Scope == "Source directory" && item.Severity == healthError && strings.Contains(strings.ToLower(item.Detail), "alias collision") {
			hasCollisionError = true
		}
		if item.Scope == "Source directory" && item.Severity == healthError {
			hasSourceDirectoryError = true
		}
	}

	if hasCollisionError {
		t.Fatalf("did not expect alias-collision error, got items: %#v", items)
	}
	if hasSourceDirectoryError {
		t.Fatalf("did not expect source-directory errors for encoded special-character variants, got items: %#v", items)
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
		{Severity: healthOK, Scope: "Source directory(s)", Detail: "C:/A"},
		{Severity: healthWarn, Scope: "Source directory(s)", Detail: "C:/B → some warning"},
		{Severity: healthOK, Scope: "Backup directory", Detail: "C:/Backup"},
	}

	var sb strings.Builder
	printStartupHealthCheck(&sb, items)
	output := sb.String()

	if strings.Count(output, "Source directory(s):") != 1 {
		t.Fatalf("expected Source directory(s) title once, got output: %q", output)
	}
	if strings.Contains(output, "[OK] Source directory(s):") {
		t.Fatalf("did not expect old inline scope format, got output: %q", output)
	}
	if !strings.Contains(output, "  [OK] C:/A") {
		t.Fatalf("expected grouped detail line for Source directory, got output: %q", output)
	}
	if !strings.Contains(output, "Backup directory:") {
		t.Fatalf("expected Backup directory title, got output: %q", output)
	}
}

func TestHealthCheckReportGroupsFindingsByScope(t *testing.T) {
	t.Parallel()

	result := buildHealthCheckResult([]healthItem{
		{Severity: healthOK, Scope: healthScopeConfig, Detail: "config loaded"},
		{Severity: healthWarn, Scope: healthScopeSourceDirectory, Detail: "Docs is empty"},
		{Severity: healthOK, Scope: healthScopeSourceDirectory, Detail: "Pics"},
		{Severity: healthError, Scope: healthScopeYubiKey, Detail: "not connected"},
		{isNote: true, Detail: "Local staging enabled."},
		{Severity: healthOK, Scope: healthScopeTempDirectory, Detail: "C:/Temp"},
	})
	var sb strings.Builder
	ui.WriteReport(&sb, result.Report())
	want := `
Startup health check
--------------------
Config:
  [OK] config loaded
Source directory(s):
  [WARN] Docs is empty
  [OK] Pics
YubiKey:
  [ERROR] not connected

Local staging enabled.
Temp directory:
  [OK] C:/Temp

Summary: 3 OK, 1 warning(s), 1 error(s)
Review the reported errors before running backup, restore, or verify.
`
	if got := sb.String(); got != want {
		t.Fatalf("unexpected report.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCheckHealthDoesNotPrint(t *testing.T) {
	exeDir := t.TempDir()
	cfg := &util.Config{BackupDirectory: filepath.Join(exeDir, "Backups"), LogLevel: "info"}
	var result HealthCheckResult
	output := testutil.CaptureStdout(t, func() {
		result = CheckHealth(cfg, exeDir, filepath.Join(exeDir, "config.yaml"))
	})
	if output != "" {
		t.Fatalf("CheckHealth must not print, got %q", output)
	}
	if r := result.Report(); r.Title != "Startup health check" || len(r.Sections) < 2 {
		t.Fatalf("unexpected report %+v", r)
	}
}
