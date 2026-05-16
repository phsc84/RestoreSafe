package startup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type healthSeverity int

const (
	healthOK healthSeverity = iota
	healthWarn
	healthError
)

const (
	healthScopeConfig          = "Config"
	healthScopeSourceFolder    = "Source folder(s)"
	healthScopeTargetFolder    = "Backup folder"
	healthScopeTempDirectory   = "Temp directory"
	healthScopeYubiKey         = "YubiKey"
	healthScopeBackupInventory = "Backup inventory"
	healthScopeBackupSet       = "Backup set"
	healthScopeChallengeFile   = "Challenge file"
)

type healthItem struct {
	Severity healthSeverity
	Scope    string
	Detail   string
	isNote   bool // printed as plain unindented text; skipped in OK/WARN/ERROR counts
}

// RunStartupHealthCheck performs a non-interactive diagnostic pass when the
// application starts. It never aborts startup; it only reports findings.
func RunStartupHealthCheck(cfg *util.Config, exeDir, configPath string) {
	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, configPath)
	printStartupHealthCheck(os.Stdout, items)
}

func collectStartupHealthItemsWithConfigPath(cfg *util.Config, exeDir, configPath string) []healthItem {
	targetDir := util.ResolveDir(cfg.TargetFolder, exeDir)
	configPathDisplay := filepath.ToSlash(filepath.Clean(configPath))
	items := make([]healthItem, 0)

	items = append(items, checkConfigFileHealth(configPathDisplay)...)

	sourceStatuses := operation.InspectSourceFoldersForValidation(cfg.SourceFolders, exeDir)
	for _, src := range sourceStatuses {
		if src.Err != nil {
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    healthScopeSourceFolder,
				Detail:   fmt.Sprintf("%s → %v", src.Resolved, src.Err),
			})
			continue
		}
		if src.Warning != "" {
			items = append(items, healthItem{
				Severity: healthWarn,
				Scope:    healthScopeSourceFolder,
				Detail:   fmt.Sprintf("%s → %s", src.Resolved, src.Warning),
			})
		} else {
			items = append(items, healthItem{
				Severity: healthOK,
				Scope:    healthScopeSourceFolder,
				Detail:   src.Resolved,
			})
		}
	}

	items = append(items, checkTargetFolderHealth(targetDir)...)
	items = append(items, checkYubiKeyHealth(cfg)...)
	items = append(items, checkBackupInventoryHealth(targetDir)...)

	firstValidSource := ""
	for _, src := range sourceStatuses {
		if src.Err == nil && !src.Skip {
			firstValidSource = src.Resolved
			break
		}
	}
	stagingPlan := operation.PlanLocalStaging(firstValidSource, targetDir, os.TempDir())
	if stagingPlan.Enabled {
		items = append(items, healthItem{
			isNote: true,
			Detail: fmt.Sprintf("Local staging enabled, because source and target folders share the same drive/share (%s).", util.VolumeDisplay(targetDir)),
		})
		items = append(items, checkTempDirHealth()...)
	}

	return items
}

func checkConfigFileHealth(configPathDisplay string) []healthItem {
	if _, err := os.Stat(configPathDisplay); err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeConfig,
			Detail:   fmt.Sprintf("%s → %v. Remedy: Ensure config.yaml exists and is readable.", configPathDisplay, err),
		}}
	}
	return []healthItem{{
		Severity: healthOK,
		Scope:    healthScopeConfig,
		Detail:   configPathDisplay,
	}}
}

func checkTargetFolderHealth(targetDir string) []healthItem {
	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []healthItem{{
				Severity: healthWarn,
				Scope:    healthScopeTargetFolder,
				Detail:   fmt.Sprintf("%s does not exist yet and will be created during backup", targetDir),
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeTargetFolder,
			Detail:   fmt.Sprintf("%s → %v. Remedy: Check target_folder in config.yaml and ensure read access.", targetDir, err),
		}}
	}

	if !info.IsDir() {
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeTargetFolder,
			Detail:   fmt.Sprintf("%s is not a directory. Remedy: Provide a folder path, not a file path.", targetDir),
		}}
	}

	return probeWriteAccess(
		targetDir,
		healthScopeTargetFolder,
		"Adjust write permissions or choose a different target_folder.",
		"Check delete permissions in target_folder.",
	)
}

func checkTempDirHealth() []healthItem {
	return probeWriteAccess(
		os.TempDir(),
		healthScopeTempDirectory,
		"Point TEMP/TMP to a writable folder or adjust permissions.",
		"Check delete permissions for TEMP/TMP.",
	)
}

// probeWriteAccess creates and removes a temporary file in dir to confirm write
// and delete access. It returns health items using the given scope and remedy strings.
func probeWriteAccess(dir, scope, writeErrRemedy, cleanupErrRemedy string) []healthItem {
	display := filepath.ToSlash(dir)
	probe, err := os.CreateTemp(dir, ".restoresafe-health-*.tmp")
	if err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    scope,
			Detail:   fmt.Sprintf("%s is not writable: %v. Remedy: %s", display, err, writeErrRemedy),
		}}
	}
	probePath := probe.Name()
	probe.Close()

	items := []healthItem{{
		Severity: healthOK,
		Scope:    scope,
		Detail:   display,
	}}
	if err := os.Remove(probePath); err != nil {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    scope,
			Detail:   fmt.Sprintf("Temporary write probe cleanup failed: %v. Remedy: %s", err, cleanupErrRemedy),
		})
	}
	return items
}

func checkYubiKeyHealth(cfg *util.Config) []healthItem {
	if !cfg.UseYubiKey() {
		return []healthItem{{
			Severity: healthOK,
			Scope:    healthScopeYubiKey,
			Detail:   "Disabled",
		}}
	}

	if err := security.CheckYubiKeyAvailability(); err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeYubiKey,
			Detail:   fmt.Sprintf("%v. Remedy: Place ykman.exe in the same folder as RestoreSafe.exe, install YubiKey Manager (v5), or add ykman to PATH. Compatibility note: RestoreSafe supports only YubiKey v5 hardware.", err),
		}}
	}

	return []healthItem{{
		Severity: healthOK,
		Scope:    healthScopeYubiKey,
		Detail:   "ykman found (application folder, PATH, or standard install directory; YubiKey v5 supported)",
	}}
}

func checkBackupInventoryHealth(targetDir string) []healthItem {
	index, err := catalog.ScanBackups(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []healthItem{{
				Severity: healthWarn,
				Scope:    healthScopeBackupInventory,
				Detail:   "Target folder does not exist yet, no backups to inspect",
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("Failed to scan backups: %v. Remedy: Check read permissions in target_folder.", err),
		}}
	}

	if len(index) == 0 {
		return []healthItem{{
			Severity: healthWarn,
			Scope:    healthScopeBackupInventory,
			Detail:   "No backup sets found. Remedy: Check target_folder or create a new backup run.",
		}}
	}

	items := []healthItem{{
		Severity: healthOK,
		Scope:    healthScopeBackupInventory,
		Detail:   fmt.Sprintf("Found %d backup set(s)", len(index)),
	}}

	for _, item := range buildBackupInventoryIssueItems(targetDir, index) {
		items = append(items, item)
	}

	return items
}

func buildBackupInventoryIssueItems(targetDir string, index []util.BackupEntry) []healthItem {
	challengeFiles, err := listChallengeFiles(targetDir)
	if err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("Failed to inspect challenge files: %v. Remedy: Check read permissions in target_folder.", err),
		}}
	}

	sorted := catalog.SortedEntries(index)
	runHasChallenge := make(map[string]bool)
	entryHasChallenge := make(map[string]bool)
	expectedChallengeFiles := make(map[string]bool)
	items := make([]healthItem, 0)
	structuralIssues := 0

	for _, entry := range sorted {
		_, _, err := catalog.InspectBackupParts(targetDir, entry)
		entryLabel := entry.String()
		if err != nil {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    healthScopeBackupSet,
				Detail:   fmt.Sprintf("%s → %v", entryLabel, err),
			})
		}

		challengeBase := filepath.Base(util.ChallengeFileName(targetDir, entry.FolderName, entry.Date, entry.ID))
		hasChallenge := challengeFiles[challengeBase]
		entryHasChallenge[entryLabel] = hasChallenge
		expectedChallengeFiles[challengeBase] = true
		runHasChallenge[entry.RunKey()] = runHasChallenge[entry.RunKey()] || hasChallenge
	}

	for _, entry := range sorted {
		if runHasChallenge[entry.RunKey()] && !entryHasChallenge[entry.String()] {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    healthScopeChallengeFile,
				Detail:   fmt.Sprintf("%s is missing its .challenge file for a YubiKey-protected backup run. Remedy: Put the matching .challenge file in the same folder as the .enc files.", entry.String()),
			})
		}
	}

	for _, orphan := range orphanChallengeFiles(challengeFiles, expectedChallengeFiles) {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    healthScopeChallengeFile,
			Detail:   fmt.Sprintf("%s has no matching backup parts. Remedy: Remove the file or restore the related backup parts.", orphan),
		})
	}

	if structuralIssues == 0 {
		items = append(items, healthItem{
			Severity: healthOK,
			Scope:    healthScopeBackupInventory,
			Detail:   "All detected backup sets are structurally complete",
		})
	}

	return items
}

func listChallengeFiles(targetDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, err
	}

	files := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".challenge") {
			files[entry.Name()] = true
		}
	}

	return files, nil
}

func orphanChallengeFiles(actual, expected map[string]bool) []string {
	orphans := make([]string, 0)
	for name := range actual {
		if !expected[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	return orphans
}

func printStartupHealthCheck(w io.Writer, items []healthItem) {
	fmt.Fprintln(w, "----------------------")
	fmt.Fprintln(w, "Startup health check")
	fmt.Fprintln(w, "----------------------")

	okCount := 0
	warnCount := 0
	errorCount := 0

	for _, item := range items {
		if item.isNote {
			continue // notes are informational only; don't count toward summary
		}
		switch item.Severity {
		case healthOK:
			okCount++
		case healthWarn:
			warnCount++
		case healthError:
			errorCount++
		}
	}

	// Separate items: regular (printed in grouped-scope table), notes (plain
	// unindented text shown after the table), temp-dir (scoped, shown after notes).
	regularItems := make([]healthItem, 0)
	noteItems := make([]healthItem, 0)
	tempDirItems := make([]healthItem, 0)
	for _, item := range items {
		switch {
		case item.isNote:
			noteItems = append(noteItems, item)
		case item.Scope == healthScopeTempDirectory:
			tempDirItems = append(tempDirItems, item)
		default:
			regularItems = append(regularItems, item)
		}
	}

	orderedScopes := make([]string, 0)
	itemsByScope := make(map[string][]healthItem)
	for _, item := range regularItems {
		if _, exists := itemsByScope[item.Scope]; !exists {
			orderedScopes = append(orderedScopes, item.Scope)
		}
		itemsByScope[item.Scope] = append(itemsByScope[item.Scope], item)
	}

	for _, scope := range orderedScopes {
		fmt.Fprintf(w, "%s:\n", scope)
		for _, item := range itemsByScope[scope] {
			fmt.Fprintf(w, "  [%s] %s\n", healthSeverityLabel(item.Severity), item.Detail)
		}
	}

	if len(noteItems) > 0 || len(tempDirItems) > 0 {
		fmt.Fprintln(w)
		for _, item := range noteItems {
			fmt.Fprintln(w, item.Detail)
		}
		if len(tempDirItems) > 0 {
			fmt.Fprintf(w, "%s:\n", healthScopeTempDirectory)
			for _, item := range tempDirItems {
				fmt.Fprintf(w, "  [%s] %s\n", healthSeverityLabel(item.Severity), item.Detail)
			}
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Summary: %d OK, %d warning(s), %d error(s)\n", okCount, warnCount, errorCount)
	if errorCount > 0 {
		fmt.Fprintln(w, "Review the reported errors before running backup, restore, or verify.")
	}
	fmt.Fprintln(w)
}

func healthSeverityLabel(severity healthSeverity) string {
	switch severity {
	case healthOK:
		return "OK"
	case healthWarn:
		return "WARN"
	case healthError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
