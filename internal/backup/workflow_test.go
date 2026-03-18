package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInspectSourceFoldersReportsExpectedStatuses(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	okDir := filepath.Join(exeDir, "ok")
	if err := os.MkdirAll(okDir, 0o750); err != nil {
		t.Fatalf("failed to create ok directory: %v", err)
	}

	filePath := filepath.Join(exeDir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to create file path: %v", err)
	}

	statuses := inspectSourceFolders([]string{"ok", "not-a-dir.txt", "missing"}, exeDir)
	if len(statuses) != 3 {
		t.Fatalf("expected 3 source statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil {
		t.Fatalf("expected first source to be valid, got error: %v", statuses[0].Err)
	}
	if statuses[1].Err == nil {
		t.Fatal("expected file path to be rejected as non-directory")
	}
	if statuses[2].Err == nil {
		t.Fatal("expected missing path to return error")
	}
}

func TestCountSourcesOnSameVolumeAsTarget(t *testing.T) {
	t.Parallel()

	targetDir := filepath.Join(t.TempDir(), "target")
	sources := []sourceFolderStatus{
		{Resolved: filepath.Join(filepath.Dir(targetDir), "source-a")},
		{Resolved: filepath.Join(filepath.Dir(targetDir), "source-b")},
		{Resolved: filepath.Join(filepath.VolumeName(targetDir)+string(filepath.Separator), "other-root"), Err: nil},
		{Resolved: filepath.Join(filepath.Dir(targetDir), "skipped"), Skip: true},
		{Resolved: filepath.Join(filepath.Dir(targetDir), "broken"), Err: os.ErrNotExist},
	}

	got := countSourcesOnSameVolumeAsTarget(targetDir, sources)
	if got < 2 {
		t.Fatalf("expected at least 2 same-volume active sources, got %d", got)
	}
	if got > 3 {
		t.Fatalf("expected skipped/error sources to be ignored, got %d", got)
	}
}

func TestInspectSourceFoldersAssignsAliasForDuplicateBasename(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root-b", "Documents")
	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}

	statuses := inspectSourceFolders([]string{first, second}, exeDir)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil || statuses[1].Err != nil {
		t.Fatalf("expected both sources to be valid, got errors: %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if statuses[0].BackupName == "" || statuses[1].BackupName == "" {
		t.Fatal("expected backup names to be assigned")
	}
	if statuses[0].BackupName == statuses[1].BackupName {
		t.Fatalf("expected distinct backup names for duplicate basenames, got %q", statuses[0].BackupName)
	}

	joined := strings.ToLower(statuses[0].BackupName + "|" + statuses[1].BackupName)
	if !strings.Contains(joined, "root~2d~a") || !strings.Contains(joined, "root~2d~b") {
		t.Fatalf("expected aliases containing encoded root~2d~a/root~2d~b, got %q and %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if !strings.Contains(joined, "-c") {
		t.Fatalf("expected aliases to include drive letter hint (e.g. -C), got %q and %q", statuses[0].BackupName, statuses[1].BackupName)
	}
}

func TestInspectSourceFoldersWarnsOnTrueIdenticalDuplicate(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	shared := filepath.Join(exeDir, "root-a", "Documents")
	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatalf("failed to create shared source: %v", err)
	}

	statuses := inspectSourceFolders([]string{shared, shared}, exeDir)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil || statuses[1].Err != nil {
		t.Fatalf("expected both statuses to be valid, got %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if statuses[0].Skip {
		t.Fatal("expected first source to stay active")
	}
	if !statuses[1].Skip {
		t.Fatal("expected second identical source to be skipped")
	}
	if statuses[1].Warning == "" || !strings.Contains(strings.ToLower(statuses[1].Warning), "identical duplicate") {
		t.Fatalf("expected duplicate warning, got %q", statuses[1].Warning)
	}
	if statuses[0].BackupName == "" || statuses[1].BackupName == "" {
		t.Fatalf("expected backup names to be assigned, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
	}
	if statuses[0].BackupName != statuses[1].BackupName {
		t.Fatalf("expected identical duplicates to share backup name, got %q / %q", statuses[0].BackupName, statuses[1].BackupName)
	}
}

func TestInspectSourceFoldersDistinguishesHyphenAndUnderscoreAliases(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root_a", "Documents")
	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}

	statuses := inspectSourceFolders([]string{first, second}, exeDir)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses[0].Err != nil || statuses[1].Err != nil {
		t.Fatalf("expected both sources to be valid, got %v / %v", statuses[0].Err, statuses[1].Err)
	}
	if statuses[0].Skip || statuses[1].Skip {
		t.Fatal("expected non-identical sources to remain active (not skipped)")
	}

	firstName := statuses[0].BackupName
	secondName := statuses[1].BackupName
	if firstName == "" || secondName == "" {
		t.Fatalf("expected backup names to be populated, got %q / %q", firstName, secondName)
	}
	if firstName == secondName {
		t.Fatalf("expected distinct aliases, got %q and %q", firstName, secondName)
	}
	firstLower := strings.ToLower(firstName)
	secondLower := strings.ToLower(secondName)
	if !(strings.Contains(firstLower, "root~2d~a-c") || strings.Contains(secondLower, "root~2d~a-c")) {
		t.Fatalf("expected one alias to include root~2d~a-c, got %q / %q", firstName, secondName)
	}
	if !(strings.Contains(firstLower, "root~5f~a-c") || strings.Contains(secondLower, "root~5f~a-c")) {
		t.Fatalf("expected one alias to include root~5f~a-c, got %q / %q", firstName, secondName)
	}
}

func TestInspectSourceFoldersDistinguishesSpaceFromHyphenAndUnderscoreAliases(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "root-a", "Documents")
	second := filepath.Join(exeDir, "root_a", "Documents")
	third := filepath.Join(exeDir, "root a", "Documents")
	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}
	if err := os.MkdirAll(third, 0o750); err != nil {
		t.Fatalf("failed to create third source: %v", err)
	}

	statuses := inspectSourceFolders([]string{first, second, third}, exeDir)
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	for i := range statuses {
		if statuses[i].Err != nil {
			t.Fatalf("expected all sources to be valid, got status[%d] error: %v", i, statuses[i].Err)
		}
		if statuses[i].Skip {
			t.Fatalf("expected all sources to remain active, status[%d] was skipped", i)
		}
	}

	joined := strings.ToLower(statuses[0].BackupName + "|" + statuses[1].BackupName + "|" + statuses[2].BackupName)
	if !strings.Contains(joined, "root~2d~a-c") {
		t.Fatalf("expected alias for root-a to contain root~2d~a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~5f~a-c") {
		t.Fatalf("expected alias for root_a to contain root~5f~a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~20~a-c") {
		t.Fatalf("expected alias for root a to contain root~20~a-c, got %q", joined)
	}
}

func TestInspectSourceFoldersEncodesSpecialCharactersUniquely(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "Root-A", "Documents")
	second := filepath.Join(exeDir, "Root_A", "Documents")
	third := filepath.Join(exeDir, "Root A", "Documents")
	fourth := filepath.Join(exeDir, "Root.A", "Documents")
	fifth := filepath.Join(exeDir, "Root~A", "Documents")
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

	statuses := inspectSourceFolders([]string{first, second, third, fourth, fifth}, exeDir)
	if len(statuses) != 5 {
		t.Fatalf("expected 5 statuses, got %d", len(statuses))
	}

	for i, status := range statuses {
		if status.Skip {
			t.Fatalf("expected status[%d] to remain active, but it was skipped", i)
		}
		if status.Err != nil {
			t.Fatalf("expected status[%d] to be valid, got error: %v", i, status.Err)
		}
	}

	joined := strings.ToLower(statuses[0].BackupName + "|" + statuses[1].BackupName + "|" + statuses[2].BackupName + "|" + statuses[3].BackupName + "|" + statuses[4].BackupName)
	if !strings.Contains(joined, "root~2d~a-c") {
		t.Fatalf("expected encoded alias fragment root~2d~a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~5f~a-c") {
		t.Fatalf("expected encoded alias fragment root~5f~a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~20~a-c") {
		t.Fatalf("expected encoded alias fragment root~20~a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~2e~a-c") {
		t.Fatalf("expected encoded alias fragment root~2e~a-c, got %q", joined)
	}
	if !strings.Contains(joined, "root~7e~a-c") {
		t.Fatalf("expected encoded alias fragment root~7e~a-c, got %q", joined)
	}
}

func TestSanitizeAliasPartEncodesNonAlphaNumericAsUTF8Hex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "Root-A", want: "Root~2D~A"},
		{input: "Root_A", want: "Root~5F~A"},
		{input: "Root A", want: "Root~20~A"},
		{input: "Root.A", want: "Root~2E~A"},
		{input: "Root~A", want: "Root~7E~A"},
	}

	for _, tc := range tests {
		if got := sanitizeAliasPart(tc.input); got != tc.want {
			t.Fatalf("sanitizeAliasPart(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidateSourceFolders(t *testing.T) {
	t.Parallel()

	valid := []sourceFolderStatus{{Resolved: "A"}, {Resolved: "B"}}
	if err := validateSourceFolders(valid); err != nil {
		t.Fatalf("expected valid sources to pass, got: %v", err)
	}

	invalid := []sourceFolderStatus{{Resolved: "A"}, {Resolved: "B", Err: os.ErrNotExist}}
	err := validateSourceFolders(invalid)
	if err == nil {
		t.Fatal("expected error for invalid source folders, got nil")
	}
}

func TestYesNo(t *testing.T) {
	t.Parallel()

	if got := yesNo(true); got != "enabled" {
		t.Fatalf("expected enabled, got %q", got)
	}
	if got := yesNo(false); got != "disabled" {
		t.Fatalf("expected disabled, got %q", got)
	}
}

func TestEstimateSelectedSourceBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcA := filepath.Join(root, "A")
	srcB := filepath.Join(root, "B")
	if err := os.MkdirAll(srcA, 0o750); err != nil {
		t.Fatalf("failed to create source A: %v", err)
	}
	if err := os.MkdirAll(srcB, 0o750); err != nil {
		t.Fatalf("failed to create source B: %v", err)
	}

	if err := os.WriteFile(filepath.Join(srcA, "one.bin"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("failed to write source A file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcB, "two.bin"), []byte("1234567890"), 0o600); err != nil {
		t.Fatalf("failed to write source B file: %v", err)
	}

	sources := []sourceFolderStatus{
		{Resolved: srcA},
		{Resolved: srcB, Skip: true},
		{Resolved: filepath.Join(root, "missing"), Err: os.ErrNotExist},
	}

	total, warnings := estimateSelectedSourceBytes(sources)
	if total != 5 {
		t.Fatalf("expected total 5 bytes from runnable source, got %d", total)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}

func TestEstimateSelectedSourceBytesWarningOnUnreadablePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-dir-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write file path: %v", err)
	}

	total, warnings := estimateSelectedSourceBytes([]sourceFolderStatus{{Resolved: filePath}})
	if total != 0 {
		t.Fatalf("expected total 0 bytes when estimation fails, got %d", total)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", warnings)
	}
}

func TestPlanBackupLocalStaging(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		source           string
		target           string
		tempDir          string
		expectEnabled    bool
		expectSameVolume bool
	}{
		{
			name:             "same-volume source and target with local temp should enable staging",
			source:           `M:\Documents`,
			target:           `M:\Backups`,
			tempDir:          `C:\Temp`,
			expectEnabled:    true,
			expectSameVolume: true,
		},
		{
			name:             "same-volume but temp on same volume should disable staging",
			source:           `M:\Documents`,
			target:           `M:\Backups`,
			tempDir:          `M:\Temp`,
			expectEnabled:    false,
			expectSameVolume: true,
		},
		{
			name:             "different volumes should disable staging",
			source:           `M:\Documents`,
			target:           `C:\Backups`,
			tempDir:          `C:\Temp`,
			expectEnabled:    false,
			expectSameVolume: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := operation.PlanLocalStaging(tt.source, tt.target, tt.tempDir)
			if plan.Enabled != tt.expectEnabled {
				t.Errorf("Expected Enabled=%v, got %v", tt.expectEnabled, plan.Enabled)
			}
			if plan.SameVolume != tt.expectSameVolume {
				t.Errorf("Expected SameVolume=%v, got %v", tt.expectSameVolume, plan.SameVolume)
			}
		})
	}
}

func TestBackupFolderLogsTarBeforeEncryption(t *testing.T) {
	tempRoot := t.TempDir()
	sourceDir := filepath.Join(tempRoot, "source")
	targetDir := filepath.Join(tempRoot, "target")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "sample.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	logPath := filepath.Join(targetDir, fmt.Sprintf("order-%d.log", time.Now().UnixNano()))
	logger, err := util.NewLogger(logPath, "debug")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 1, IODiagnostics: false}
	_, backupErr := backupFolder(sourceDir, filepath.Base(sourceDir), targetDir, "2026-03-18", util.BackupID("ORD123"), []byte("pw"), cfg, logger)
	logger.Close()
	if backupErr != nil {
		t.Fatalf("backupFolder failed: %v", backupErr)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	tarLine := "Starting TAR creation for: " + sourceDir
	encryptLine := "Starting encryption..."
	tarIndex := strings.Index(logContent, tarLine)
	encryptIndex := strings.Index(logContent, encryptLine)
	if tarIndex < 0 {
		t.Fatalf("expected TAR creation debug line in log, got: %q", logContent)
	}
	if encryptIndex < 0 {
		t.Fatalf("expected encryption debug line in log, got: %q", logContent)
	}
	if tarIndex > encryptIndex {
		t.Fatalf("expected TAR creation log before encryption log, got log: %q", logContent)
	}
}

func TestPrintBackupPreflightPlacesSameVolumeWarningAfterSourceFolder(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	targetDir := filepath.Join(tempRoot, "target")
	sourceDir := filepath.Join(tempRoot, "source")
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	cfg := &util.Config{SplitSizeMB: 64, RetentionKeep: 0, AuthenticationMode: util.AuthModePassword, LogLevel: "debug"}
	sources := []sourceFolderStatus{{Resolved: sourceDir}}
	stagingPlan := operation.LocalStagingPlan{Enabled: false, SameVolume: true}

	output := captureStdout(t, func() {
		printBackupPreflight(cfg, targetDir, sources, stagingPlan)
	})

	sourceLine := "[OK]    " + sourceDir
	warnLinePrefix := "[WARN]  Source folder warning: Source and target folders are on the same drive/share"
	sourceIndex := strings.Index(output, sourceLine)
	warnIndex := strings.Index(output, warnLinePrefix)
	if sourceIndex < 0 {
		t.Fatalf("expected source line in output, got: %q", output)
	}
	if warnIndex < 0 {
		t.Fatalf("expected same-volume warning line in output, got: %q", output)
	}
	if warnIndex < sourceIndex {
		t.Fatalf("expected warning after source folder line, got output: %q", output)
	}

	headerIndex := strings.Index(output, "Source folders:")
	if headerIndex < 0 {
		t.Fatalf("expected Source folders header, got: %q", output)
	}
	if strings.Contains(output[:headerIndex], warnLinePrefix) {
		t.Fatalf("did not expect same-volume warning before source folder list, got output: %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stdout writer: %v", err)
	}
	os.Stdout = originalStdout

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close stdout reader: %v", err)
	}

	return string(data)
}
