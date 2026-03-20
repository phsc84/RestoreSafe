package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanBackupSourcesReportsExpectedStatuses(t *testing.T) {
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

	plans := planBackupSources([]string{"ok", "not-a-dir.txt", "missing"}, exeDir)
	if len(plans) != 3 {
		t.Fatalf("expected 3 source plans, got %d", len(plans))
	}

	if plans[0].Err != nil {
		t.Fatalf("expected first source to be valid, got error: %v", plans[0].Err)
	}
	if plans[1].Err == nil {
		t.Fatal("expected file path to be rejected as non-directory")
	}
	if plans[2].Err == nil {
		t.Fatal("expected missing path to return error")
	}
	if plans[0].BackupName != "ok" {
		t.Fatalf("expected first source backup name ok, got %q", plans[0].BackupName)
	}
	if plans[1].BackupName != "not-a-dir.txt" {
		t.Fatalf("expected file-path backup name fallback, got %q", plans[1].BackupName)
	}
}

func TestPlanBackupSourcesAssignsBaseNamesForUniqueFolders(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	first := filepath.Join(exeDir, "One")
	second := filepath.Join(exeDir, "Two")
	if err := os.MkdirAll(first, 0o750); err != nil {
		t.Fatalf("failed to create first source: %v", err)
	}
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("failed to create second source: %v", err)
	}

	plans := planBackupSources([]string{first, second}, exeDir)
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	for i, plan := range plans {
		if plan.Err != nil {
			t.Fatalf("expected plan[%d] to be valid, got error: %v", i, plan.Err)
		}
		if plan.Skip {
			t.Fatalf("expected plan[%d] to stay active, got skipped", i)
		}
		if plan.BackupName == "" {
			t.Fatalf("expected plan[%d] to have backup name", i)
		}
	}

	if plans[0].BackupName != "One" {
		t.Fatalf("expected first backup name One, got %q", plans[0].BackupName)
	}
	if plans[1].BackupName != "Two" {
		t.Fatalf("expected second backup name Two, got %q", plans[1].BackupName)
	}
}

func TestPlanBackupSourcesAssignsAliasForDuplicateBasename(t *testing.T) {
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

	plans := planBackupSources([]string{first, second}, exeDir)
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	if plans[0].Err != nil || plans[1].Err != nil {
		t.Fatalf("expected both sources to be valid, got errors: %v / %v", plans[0].Err, plans[1].Err)
	}
	if plans[0].BackupName == "" || plans[1].BackupName == "" {
		t.Fatal("expected backup names to be assigned")
	}
	if plans[0].BackupName == plans[1].BackupName {
		t.Fatalf("expected distinct backup names for duplicate basenames, got %q", plans[0].BackupName)
	}

	joined := strings.ToLower(plans[0].BackupName + "|" + plans[1].BackupName)
	if !strings.Contains(joined, "root~2d~a") || !strings.Contains(joined, "root~2d~b") {
		t.Fatalf("expected aliases containing encoded root~2d~a/root~2d~b, got %q and %q", plans[0].BackupName, plans[1].BackupName)
	}
	if !strings.Contains(joined, "-c") {
		t.Fatalf("expected aliases to include drive letter hint (e.g. -C), got %q and %q", plans[0].BackupName, plans[1].BackupName)
	}
}

func TestPlanBackupSourcesMarksIdenticalDuplicateAsSkipped(t *testing.T) {
	t.Parallel()

	exeDir := t.TempDir()
	shared := filepath.Join(exeDir, "Docs")
	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatalf("failed to create shared source: %v", err)
	}

	plans := planBackupSources([]string{shared, shared}, exeDir)
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}
	if plans[0].Skip {
		t.Fatal("expected first source to remain active")
	}
	if !plans[1].Skip {
		t.Fatal("expected duplicate source to be skipped")
	}
	if !strings.Contains(strings.ToLower(plans[1].Warning), "identical duplicate") {
		t.Fatalf("expected duplicate warning, got %q", plans[1].Warning)
	}
	if plans[0].BackupName == "" || plans[1].BackupName == "" {
		t.Fatalf("expected both plans to have backup names, got %q / %q", plans[0].BackupName, plans[1].BackupName)
	}
	if plans[0].BackupName != plans[1].BackupName {
		t.Fatalf("expected duplicate plans to share backup name, got %q / %q", plans[0].BackupName, plans[1].BackupName)
	}
}

func TestPlanBackupSourcesDistinguishesHyphenAndUnderscoreAliases(t *testing.T) {
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

	plans := planBackupSources([]string{first, second}, exeDir)
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	if plans[0].Err != nil || plans[1].Err != nil {
		t.Fatalf("expected both sources to be valid, got %v / %v", plans[0].Err, plans[1].Err)
	}
	if plans[0].Skip || plans[1].Skip {
		t.Fatal("expected non-identical sources to remain active (not skipped)")
	}

	firstName := plans[0].BackupName
	secondName := plans[1].BackupName
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

func TestPlanBackupSourcesDistinguishesSpaceFromHyphenAndUnderscoreAliases(t *testing.T) {
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

	plans := planBackupSources([]string{first, second, third}, exeDir)
	if len(plans) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(plans))
	}

	for i := range plans {
		if plans[i].Err != nil {
			t.Fatalf("expected all sources to be valid, got plan[%d] error: %v", i, plans[i].Err)
		}
		if plans[i].Skip {
			t.Fatalf("expected all sources to remain active, plan[%d] was skipped", i)
		}
	}

	joined := strings.ToLower(plans[0].BackupName + "|" + plans[1].BackupName + "|" + plans[2].BackupName)
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

func TestPlanBackupSourcesEncodesSpecialCharactersUniquely(t *testing.T) {
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

	plans := planBackupSources([]string{first, second, third, fourth, fifth}, exeDir)
	if len(plans) != 5 {
		t.Fatalf("expected 5 plans, got %d", len(plans))
	}

	for i, plan := range plans {
		if plan.Skip {
			t.Fatalf("expected plan[%d] to remain active, but it was skipped", i)
		}
		if plan.Err != nil {
			t.Fatalf("expected plan[%d] to be valid, got error: %v", i, plan.Err)
		}
	}

	joined := strings.ToLower(plans[0].BackupName + "|" + plans[1].BackupName + "|" + plans[2].BackupName + "|" + plans[3].BackupName + "|" + plans[4].BackupName)
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
