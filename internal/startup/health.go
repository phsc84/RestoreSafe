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
)

type healthSeverity int

const (
	healthOK healthSeverity = iota
	healthWarn
	healthError
)

const (
	healthScopeConfig          = "Config"
	healthScopeArgon2          = "Argon2 settings"
	healthScopeSourceDirectory = "Source directory(s)"
	healthScopeBackupDirectory = "Backup directory"
	healthScopeTempDirectory   = "Temp directory"
	healthScopeYubiKey         = "YubiKey"
	healthScopeBackupInventory = "Backup inventory"
	healthScopeBackupSet       = "Backup set"
	healthScopeKeys            = "Keys"
)

type healthItem struct {
	Severity healthSeverity
	Scope    string
	Detail   string
	isNote   bool // printed as plain unindented text; skipped in OK/WARN/ERROR counts
}

// HealthCheckResult captures which health error scopes were detected at startup.
type HealthCheckResult struct {
	errorScopes map[string]bool
}

// BlocksBackup reports whether any health check error prevents running a backup.
func (r HealthCheckResult) BlocksBackup() bool {
	return r.errorScopes[healthScopeConfig] ||
		r.errorScopes[healthScopeSourceDirectory] ||
		r.errorScopes[healthScopeBackupDirectory] ||
		r.errorScopes[healthScopeYubiKey] ||
		r.errorScopes[healthScopeTempDirectory]
}

// BlocksRestoreOrVerify reports whether any health check error prevents restore or verify.
func (r HealthCheckResult) BlocksRestoreOrVerify() bool {
	return r.errorScopes[healthScopeConfig] ||
		r.errorScopes[healthScopeBackupDirectory] ||
		r.errorScopes[healthScopeYubiKey]
}

func buildHealthCheckResult(items []healthItem) HealthCheckResult {
	scopes := make(map[string]bool)
	for _, item := range items {
		if item.Severity == healthError && !item.isNote {
			scopes[item.Scope] = true
		}
	}
	return HealthCheckResult{errorScopes: scopes}
}

// RunStartupHealthCheck performs a non-interactive diagnostic pass when the
// application starts. It never aborts startup; it only reports findings.
func RunStartupHealthCheck(cfg *util.Config, exeDir, configPath string) HealthCheckResult {
	items := collectStartupHealthItemsWithConfigPath(cfg, exeDir, configPath)
	printStartupHealthCheck(os.Stdout, items)
	return buildHealthCheckResult(items)
}

func collectStartupHealthItemsWithConfigPath(cfg *util.Config, exeDir, configPath string) []healthItem {
	backupDir := util.ResolveDir(cfg.BackupDirectory, exeDir)
	configPathDisplay := filepath.ToSlash(filepath.Clean(configPath))
	items := make([]healthItem, 0)

	items = append(items, checkConfigFileHealth(configPathDisplay)...)
	items = append(items, checkArgon2Health(cfg)...)

	sourceStatuses := operation.InspectSourceDirectoriesForValidation(cfg.SourceDirectories, exeDir)
	for _, src := range sourceStatuses {
		if src.Err != nil {
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    healthScopeSourceDirectory,
				Detail:   fmt.Sprintf("%s → %v", src.Resolved, src.Err),
			})
			continue
		}
		if src.Warning != "" {
			items = append(items, healthItem{
				Severity: healthWarn,
				Scope:    healthScopeSourceDirectory,
				Detail:   fmt.Sprintf("%s → %s", src.Resolved, src.Warning),
			})
		} else {
			items = append(items, healthItem{
				Severity: healthOK,
				Scope:    healthScopeSourceDirectory,
				Detail:   src.Resolved,
			})
		}
	}

	items = append(items, checkBackupDirectoryHealth(backupDir)...)
	items = append(items, checkYubiKeyHealth(cfg)...)
	items = append(items, checkBackupInventoryHealth(cfg, backupDir)...)

	// Prefer a source that shares the target volume so staging is detected when
	// only some sources are on the same drive as the target (mirrors backup/workflow.go).
	stagingSourceDir := ""
	for _, src := range sourceStatuses {
		if src.Err == nil && !src.Skip {
			if stagingSourceDir == "" {
				stagingSourceDir = src.Resolved
			}
			if util.SameVolume(src.Resolved, backupDir) {
				stagingSourceDir = src.Resolved
				break
			}
		}
	}
	stagingPlan := operation.PlanLocalStaging(stagingSourceDir, backupDir, os.TempDir())
	if stagingPlan.Enabled {
		items = append(items, healthItem{
			isNote: true,
			Detail: fmt.Sprintf("Local staging via temp directory enabled, because source directory(s) and backup directory share the same drive (%s).", util.VolumeDisplay(backupDir)),
		})
		items = append(items, checkTempDirHealth()...)
	}

	return items
}

// checkArgon2Health surfaces a warning for each argon2 value that Load clamped
// to its enforced maximum, so the user knows the configured value was capped.
func checkArgon2Health(cfg *util.Config) []healthItem {
	items := make([]healthItem, 0, len(cfg.Argon2Notices))
	for _, notice := range cfg.Argon2Notices {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    healthScopeArgon2,
			Detail:   notice + " Remedy: Lower the value in config.yaml to silence this warning.",
		})
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

func checkBackupDirectoryHealth(backupDir string) []healthItem {
	info, err := os.Stat(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []healthItem{{
				Severity: healthWarn,
				Scope:    healthScopeBackupDirectory,
				Detail:   fmt.Sprintf("%s does not exist yet and will be created during backup", backupDir),
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeBackupDirectory,
			Detail:   fmt.Sprintf("%s → %v. Remedy: Check backup_directory in config.yaml and ensure read access.", backupDir, err),
		}}
	}

	if !info.IsDir() {
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeBackupDirectory,
			Detail:   fmt.Sprintf("%s is not a directory. Remedy: Provide a directory path, not a file path.", backupDir),
		}}
	}

	return probeWriteAccess(
		backupDir,
		healthScopeBackupDirectory,
		"Adjust write permissions or choose a different backup_directory.",
		"Check delete permissions in backup_directory.",
	)
}

func checkTempDirHealth() []healthItem {
	return probeWriteAccess(
		os.TempDir(),
		healthScopeTempDirectory,
		"Point TEMP/TMP to a writable directory or adjust permissions.",
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

	if err := security.CheckYubiKeyConnected(); err != nil {
		return []healthItem{{
			Severity: healthWarn,
			Scope:    healthScopeYubiKey,
			Detail:   "YubiKey not connected. Remedy: Connect the YubiKey before running backup, restore, or verify.",
		}}
	}
	return []healthItem{{
		Severity: healthOK,
		Scope:    healthScopeYubiKey,
		Detail:   "YubiKey connected",
	}}
}

func checkBackupInventoryHealth(cfg *util.Config, backupDir string) []healthItem {
	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []healthItem{{
				Severity: healthWarn,
				Scope:    healthScopeBackupInventory,
				Detail:   "Backup directory does not exist yet, no backups to inspect",
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("Failed to scan backups: %v. Remedy: Check read permissions in backup directory.", err),
		}}
	}

	items := make([]healthItem, 0)
	items = append(items, checkLegacyAndTempFiles(backupDir)...)

	if len(infos) == 0 {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    healthScopeBackupInventory,
			Detail:   "No backup sets found. Remedy: Check backup directory or create a new backup run.",
		})
		return append(items, checkKeyHealth(cfg, infos)...)
	}

	items = append(items, healthItem{
		Severity: healthOK,
		Scope:    healthScopeBackupInventory,
		Detail:   fmt.Sprintf("Found %d backup set(s)", len(infos)),
	})
	items = append(items, buildBackupInventoryIssueItems(infos)...)
	return append(items, checkKeyHealth(cfg, infos)...)
}

// checkLegacyAndTempFiles warns about RestoreSafe 1.x backups (which 2.0
// cannot restore and never touches) and about leftovers of interrupted
// backups (removed at the start of the next backup).
func checkLegacyAndTempFiles(backupDir string) []healthItem {
	var items []healthItem
	if legacy, err := catalog.ListLegacyFiles(backupDir); err == nil && len(legacy) > 0 {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("RestoreSafe 1.x backups found (%d file(s)). RestoreSafe 2.0 cannot restore them. Remedy: Keep RestoreSafe 1.0.2 to restore these files; they are never modified or deleted by 2.0.", len(legacy)),
		})
	}
	if temps, err := catalog.ListTempParts(backupDir); err == nil && len(temps) > 0 {
		items = append(items, healthItem{
			Severity: healthWarn,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("%d leftover file(s) of an interrupted backup found (*.enc.tmp). They are removed at the start of the next backup.", len(temps)),
		})
	}
	return items
}

func buildBackupInventoryIssueItems(infos []catalog.SetInfo) []healthItem {
	items := make([]healthItem, 0)
	structuralIssues := 0

	completeFulls := make(map[string]bool)
	for _, info := range infos {
		if info.Complete() && !info.Entry.IsDiff() {
			completeFulls[info.Entry.ChainKey()] = true
		}
	}

	for _, info := range infos {
		if info.Err != nil {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    healthScopeBackupSet,
				Detail:   fmt.Sprintf("%s → %v", info.Entry.String(), info.Err),
			})
			continue
		}
		if info.Entry.IsDiff() && !completeFulls[info.Entry.ChainKey()] {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Scope:    healthScopeBackupSet,
				Detail:   fmt.Sprintf("%s cannot be restored: the full backup [%s]_%s_*_FULL-*.enc of chain %s is missing or incomplete. Remedy: Restore the FULL files of %s from your copy, or delete the DIFF files of %s.", info.Entry.String(), info.Entry.DirectoryName, info.Entry.ChainID, info.Entry.ChainID, info.Entry.ChainID, info.Entry.ChainID),
			})
		}
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

// checkKeyHealth summarizes the current keys and whether the configuration
// requires new keys at the next backup.
func checkKeyHealth(cfg *util.Config, infos []catalog.SetInfo) []healthItem {
	ks := catalog.CurrentKeySet(infos)
	if ks == nil {
		return []healthItem{{
			Severity: healthOK,
			Scope:    healthScopeKeys,
			Detail:   "No keys yet; the next backup creates new keys",
		}}
	}
	detail := fmt.Sprintf("Current keys created %s (%s)", ks.Created().Local().Format("2006-01-02"), util.AuthMode(ks.AuthMode).Label())
	if ks.AuthMode != int(cfg.AuthenticationMode) {
		return []healthItem{{
			Severity: healthWarn,
			Scope:    healthScopeKeys,
			Detail:   detail + ". authentication_mode in config.yaml differs: the next backup creates new keys and full backups.",
		}}
	}
	return []healthItem{{Severity: healthOK, Scope: healthScopeKeys, Detail: detail}}
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
