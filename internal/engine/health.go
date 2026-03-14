package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows"
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
	targetDir := resolveDir(cfg.TargetFolder, exeDir)
	items := make([]healthItem, 0)

	items = append(items, healthItem{
		Severity: healthOK,
		Scope:    "Config",
		Detail:   fmt.Sprintf("Loaded %d source folder(s), target=%s, split size=%d MB, retention_keep=%d", len(cfg.SourceFolders), targetDir, cfg.SplitSizeMB, cfg.RetentionKeep),
	})

	sourceStatuses := inspectSourceFolders(cfg.SourceFolders, exeDir)
	for _, src := range sourceStatuses {
		if src.Err != nil {
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    "Source folder",
				Detail:   fmt.Sprintf("%s -> %v", src.Resolved, src.Err),
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
			Detail:   fmt.Sprintf("%s -> %v", targetDir, err),
		}}
	}

	if !info.IsDir() {
		return []healthItem{{
			Severity: healthError,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("%s is not a directory", targetDir),
		}}
	}

	probe, err := os.CreateTemp(targetDir, ".restoresafe-health-*.tmp")
	if err != nil {
		return []healthItem{{
			Severity: healthError,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("%s is not writable: %v", targetDir, err),
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

	freeBytes, err := queryFreeSpaceBytes(targetDir)
	if err != nil {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    "Target folder",
			Detail:   fmt.Sprintf("Free disk space could not be determined: %v", err),
		})
		return items
	}

	items = append(items, healthItem{
		Severity: healthOK,
		Scope:    "Target folder",
		Detail:   fmt.Sprintf("Free disk space: %s", formatBytesBinary(freeBytes)),
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
			Detail:   fmt.Sprintf("%s is not writable: %v", tempDir, err),
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
	if !cfg.YubikeyEnable {
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
	index, err := scanBackups(targetDir)
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
			Detail:   fmt.Sprintf("Failed to scan backups: %v", err),
		}}
	}

	if len(index) == 0 {
		return []healthItem{{
			Severity: healthWarn,
			Scope:    "Backup inventory",
			Detail:   "No backup sets found",
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
			Detail:   fmt.Sprintf("Failed to inspect challenge files: %v", err),
		}}
	}

	sorted := sortedEntries(index)
	runHasChallenge := make(map[string]bool)
	entryHasChallenge := make(map[string]bool)
	expectedChallengeFiles := make(map[string]bool)
	items := make([]healthItem, 0)
	structuralIssues := 0

	for _, entry := range sorted {
		_, _, err := inspectBackupParts(targetDir, entry)
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
				Detail:   fmt.Sprintf("%s is missing its .challenge file for a YubiKey-protected backup run", entry.String()),
			})
		}
	}

	for _, orphan := range orphanChallengeFiles(challengeFiles, expectedChallengeFiles) {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    "Challenge file",
			Detail:   fmt.Sprintf("%s has no matching backup parts", orphan),
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

func formatBytesBinary(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := float64(unit), 0
	value := float64(bytes)
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	return fmt.Sprintf("%.2f %s", value/div, units[exp])
}

func queryFreeSpaceBytes(path string) (uint64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to encode path: %w", err)
	}

	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		return 0, fmt.Errorf("Failed to query free space for %q: %w", path, err)
	}

	return freeBytesAvailable, nil
}
