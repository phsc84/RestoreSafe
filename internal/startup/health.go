package startup

import (
	"RestoreSafe/internal/backup"
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
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

type healthItem struct {
	Severity healthSeverity
	Scope    string
	Detail   string
}

// RunStartupHealthCheck performs a non-interactive diagnostic pass when the
// application starts. It never aborts startup; it only reports findings.
func RunStartupHealthCheck(cfg *util.Config, exeDir string) {
	items := collectStartupHealthItems(cfg, exeDir)
	printStartupHealthCheck(items)
}

func collectStartupHealthItems(cfg *util.Config, exeDir string) []healthItem {
	targetDir := util.ResolveDir(cfg.TargetFolder, exeDir)
	items := make([]healthItem, 0)

	items = append(items, healthItem{
		Severity: healthOK,
		Scope:    "Config",
		Detail:   fmt.Sprintf("Loaded %d source folder(s), target=%s, split size=%d MB, retention_keep=%d", len(cfg.SourceFolders), targetDir, cfg.SplitSizeMB, cfg.RetentionKeep),
	})

	sourceStatuses := backup.InspectSourceFolders(cfg.SourceFolders, exeDir)
	for _, src := range sourceStatuses {
		if src.Err != nil {
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    "Source folder",
				Detail:   fmt.Sprintf("%s -> %v", src.Resolved, src.Err),
			})
			continue
		}
		if src.Warning != "" {
			items = append(items, healthItem{
				Severity: healthWarn,
				Scope:    "Source folder",
				Detail:   fmt.Sprintf("%s -> %s", src.Resolved, src.Warning),
			})
			continue
		}
		items = append(items, healthItem{
			Severity: healthOK,
			Scope:    "Source folder",
			Detail:   src.Resolved,
		})
	}

	items = append(items, checkTargetFolderHealth(targetDir)...)
	items = append(items, checkTempDirHealth()...)
	items = append(items, checkYubiKeyHealth(cfg)...)
	items = append(items, checkBackupInventoryHealth(targetDir)...)

	return items
}

func checkTargetFolderHealth(targetDir string) []healthItem {
	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []healthItem{{
				Severity: healthWarn,
				Scope:    "Target folder",
				Detail:   fmt.Sprintf("%s does not exist yet and will be created during backup", targetDir),
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("%s -> %v. Remedy: Check target_folder in config.yaml and ensure read access.", targetDir, err),
		}}
	}

	if !info.IsDir() {
		return []healthItem{{
			Severity: healthError,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("%s is not a directory. Remedy: Provide a folder path, not a file path.", targetDir),
		}}
	}

	probe, err := os.CreateTemp(targetDir, ".restoresafe-health-*.tmp")
	if err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("%s is not writable: %v. Remedy: Adjust write permissions or choose a different target_folder.", targetDir, err),
		}}
	}
	probePath := probe.Name()
	probe.Close()
	_ = os.Remove(probePath)

	items := []healthItem{{
		Severity: healthOK,
		Scope:    "Target folder",
		Detail:   fmt.Sprintf("%s exists and is writable", targetDir),
	}}

	freeBytes, err := util.QueryFreeSpaceBytes(targetDir)
	if err != nil {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("Free disk space could not be determined: %v. Remedy: Check drive availability and Windows permissions.", err),
		})
		return items
	}

	items = append(items, healthItem{
		Severity: healthOK,
		Scope:    "Target folder",
		Detail:   fmt.Sprintf("Free disk space: %s", util.FormatBytesBinary(freeBytes)),
	})

	return items
}

func checkTempDirHealth() []healthItem {
	tempDir := os.TempDir()
	probe, err := os.CreateTemp(tempDir, ".restoresafe-health-*.tmp")
	if err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    "Temp directory",
			Detail:   fmt.Sprintf("%s is not writable: %v. Remedy: Point TEMP/TMP to a writable folder or adjust permissions.", tempDir, err),
		}}
	}
	probePath := probe.Name()
	probe.Close()
	_ = os.Remove(probePath)

	return []healthItem{{
		Severity: healthOK,
		Scope:    "Temp directory",
		Detail:   fmt.Sprintf("%s is writable", tempDir),
	}}
}

func checkYubiKeyHealth(cfg *util.Config) []healthItem {
	if !cfg.UseYubiKey() {
		return []healthItem{{
			Severity: healthOK,
			Scope:    "YubiKey",
			Detail:   "Disabled in config.yaml",
		}}
	}

	if err := security.CheckYubiKeyAvailability(); err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    "YubiKey",
			Detail:   err.Error(),
		}}
	}

	return []healthItem{{
		Severity: healthOK,
		Scope:    "YubiKey",
		Detail:   "ykchalresp found on PATH",
	}}
}

func checkBackupInventoryHealth(targetDir string) []healthItem {
	index, err := catalog.ScanBackups(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []healthItem{{
				Severity: healthWarn,
				Scope:    "Backup inventory",
				Detail:   "Target folder does not exist yet, no backups to inspect",
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Scope:    "Backup inventory",
			Detail:   fmt.Sprintf("Failed to scan backups: %v. Remedy: Check read permissions in target_folder.", err),
		}}
	}

	if len(index) == 0 {
		return []healthItem{{
			Severity: healthWarn,
			Scope:    "Backup inventory",
			Detail:   "No backup sets found. Remedy: Check target_folder or create a new backup run.",
		}}
	}

	items := []healthItem{{
		Severity: healthOK,
		Scope:    "Backup inventory",
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
			Scope:    "Backup inventory",
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
				Scope:    "Backup set",
				Detail:   fmt.Sprintf("%s -> %v", entryLabel, err),
			})
		}

		challengeBase := filepath.Base(util.ChallengeFileName(targetDir, entry.FolderName, entry.Date, entry.ID))
		hasChallenge := challengeFiles[challengeBase]
		entryHasChallenge[entryLabel] = hasChallenge
		expectedChallengeFiles[challengeBase] = true
		runHasChallenge[runKey(entry)] = runHasChallenge[runKey(entry)] || hasChallenge
	}

	for _, entry := range sorted {
		if runHasChallenge[runKey(entry)] && !entryHasChallenge[entry.String()] {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    "Challenge file",
				Detail:   fmt.Sprintf("%s is missing its .challenge file for a YubiKey-protected backup run. Remedy: Put the matching .challenge file in the same folder as the .enc files.", entry.String()),
			})
		}
	}

	for _, orphan := range orphanChallengeFiles(challengeFiles, expectedChallengeFiles) {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    "Challenge file",
			Detail:   fmt.Sprintf("%s has no matching backup parts. Remedy: Remove the file or restore the related backup parts.", orphan),
		})
	}

	if structuralIssues == 0 {
		items = append(items, healthItem{
			Severity: healthOK,
			Scope:    "Backup inventory",
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

func runKey(entry util.BackupEntry) string {
	return entry.Date + "|" + string(entry.ID)
}

func printStartupHealthCheck(items []healthItem) {
	fmt.Println()
	fmt.Println("Startup health check")
	fmt.Println("--------------------")

	okCount := 0
	warnCount := 0
	errorCount := 0

	for _, item := range items {
		label := healthSeverityLabel(item.Severity)
		fmt.Printf("[%s] %s: %s\n", label, item.Scope, item.Detail)
		switch item.Severity {
		case healthOK:
			okCount++
		case healthWarn:
			warnCount++
		case healthError:
			errorCount++
		}
	}

	fmt.Println()
	fmt.Printf("Summary: %d OK, %d warning(s), %d error(s)\n", okCount, warnCount, errorCount)
	if errorCount > 0 {
		fmt.Println("Review the reported errors before running backup, restore, or verify.")
	}
	fmt.Println()
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
		return "INFO"
	}
}
