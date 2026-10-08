// Package health runs the startup health check (configuration, directories,
// YubiKey, backup inventory and keys) and computes the snapshot of the
// backup state that the user interface shows.
package health

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/job"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
)

// Injectable for tests.
var (
	checkYubiKeyConnected = yubikey.CheckConnected
	queryDiskSpace        = fsx.QueryDiskSpace
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
	healthScopeYubiKey         = "YubiKey"
	healthScopeBackupInventory = "Backup inventory"
	healthScopeBackupSet       = "Backup set"
	healthScopeKeys            = "Keys"
)

// healthItem is one finding. Every warning and error carries a Code.
type healthItem struct {
	Severity healthSeverity
	Code     interact.Code
	Scope    string
	Detail   string
}

// Result captures the findings of the startup health check and
// which error scopes block an operation.
type Result struct {
	errorScopes map[string]bool
	items       []healthItem
}

// BlocksBackup reports whether any health check error prevents running a backup.
func (r Result) BlocksBackup() bool {
	return r.errorScopes[healthScopeConfig] ||
		r.errorScopes[healthScopeSourceDirectory] ||
		r.errorScopes[healthScopeBackupDirectory] ||
		r.errorScopes[healthScopeYubiKey]
}

// BlocksRestoreOrVerify reports whether any health check error prevents restore or verify.
func (r Result) BlocksRestoreOrVerify() bool {
	return r.errorScopes[healthScopeConfig] ||
		r.errorScopes[healthScopeBackupDirectory] ||
		r.errorScopes[healthScopeYubiKey]
}

func buildResult(items []healthItem) Result {
	scopes := make(map[string]bool)
	for _, item := range items {
		if item.Severity == healthError {
			scopes[item.Scope] = true
		}
	}
	return Result{errorScopes: scopes, items: items}
}

// Check performs the startup health check without printing it; the
// result's Report describes the findings.
func Check(cfg *config.Config, exeDir, configPath string) Result {
	return buildResult(inspect(cfg, exeDir, configPath).items)
}

// Report describes the findings as a report: one heading per checked scope
// with its items, then the summary. The findings are items, not issues;
// BlocksBackup and BlocksRestoreOrVerify decide what they block.
func (r Result) Report() interact.Report {
	var checks []interact.Row
	scopeRows := make(map[string][]interact.Row)
	var scopes []string
	okCount, warnCount, errorCount := 0, 0, 0
	for _, item := range r.items {
		switch item.Severity {
		case healthOK:
			okCount++
		case healthWarn:
			warnCount++
		case healthError:
			errorCount++
		}
		row := interact.Item(healthStatus(item.Severity), item.Detail)
		if _, seen := scopeRows[item.Scope]; !seen {
			scopes = append(scopes, item.Scope)
		}
		scopeRows[item.Scope] = append(scopeRows[item.Scope], row)
	}
	for _, scope := range scopes {
		checks = append(checks, interact.Heading(scope))
		checks = append(checks, scopeRows[scope]...)
	}

	summary := []interact.Row{interact.Note(fmt.Sprintf("Summary: %d OK, %d warning(s), %d error(s)", okCount, warnCount, errorCount))}
	if errorCount > 0 {
		summary = append(summary, interact.Note("Review the reported errors before running backup, restore, or verify."))
	}
	return interact.Report{Title: "Startup health check", Sections: []interact.Section{{Rows: checks}, {Rows: summary}}}
}

func healthStatus(severity healthSeverity) interact.Status {
	switch severity {
	case healthOK:
		return interact.StatusOK
	case healthWarn:
		return interact.StatusWarn
	default:
		return interact.StatusError
	}
}

// inspection is what the health check read, once, for the findings and for
// the snapshot.
type inspection struct {
	items      []healthItem
	configPath string
	backupDir  string
	sources    []plan.Source
	inventory  inventory
	// yubiKeyConnected is nil when the configuration uses no YubiKey.
	yubiKeyConnected *bool
}

// inventory is the content of the backup directory.
type inventory struct {
	infos []catalog.SetInfo
	// err is set when the directory could not be scanned.
	err           error
	legacy, temps []string
}

func inspect(cfg *config.Config, exeDir, configPath string) inspection {
	in := inspection{
		configPath: filepath.ToSlash(filepath.Clean(configPath)),
		backupDir:  fsx.ResolveDir(cfg.BackupDirectory, exeDir),
		sources:    plan.ResolveSources(cfg.SourceDirectories, exeDir),
	}
	in.items = append(in.items, checkConfigFileHealth(in.configPath)...)
	in.items = append(in.items, checkArgon2Health(cfg)...)
	in.items = append(in.items, sourceItems(in.sources)...)
	in.items = append(in.items, checkBackupDirectoryHealth(in.backupDir)...)

	yubiKeyItems, connected := checkYubiKeyHealth(cfg)
	in.items = append(in.items, yubiKeyItems...)
	in.yubiKeyConnected = connected

	in.inventory = scanInventory(in.backupDir)
	in.items = append(in.items, inventoryItems(cfg, in.inventory)...)
	return in
}

func collectStartupHealthItemsWithConfigPath(cfg *config.Config, exeDir, configPath string) []healthItem {
	return inspect(cfg, exeDir, configPath).items
}

func sourceItems(sources []plan.Source) []healthItem {
	items := make([]healthItem, 0, len(sources))
	for _, src := range sources {
		switch {
		case src.Err != nil:
			items = append(items, healthItem{
				Severity: healthError,
				Code:     job.SourceProblemCode(src.Err),
				Scope:    healthScopeSourceDirectory,
				Detail:   fmt.Sprintf("%s → %v", src.Resolved, src.Err),
			})
		case src.Warning != "":
			items = append(items, healthItem{
				Severity: healthWarn,
				Code:     interact.CodeSourceDuplicate,
				Scope:    healthScopeSourceDirectory,
				Detail:   fmt.Sprintf("%s → %s", src.Resolved, src.Warning),
			})
		default:
			items = append(items, healthItem{
				Severity: healthOK,
				Scope:    healthScopeSourceDirectory,
				Detail:   src.Resolved,
			})
		}
	}
	return items
}

// checkArgon2Health surfaces a warning for each argon2 value that Load clamped
// to its enforced maximum, so the user knows the configured value was capped.
func checkArgon2Health(cfg *config.Config) []healthItem {
	items := make([]healthItem, 0, len(cfg.Argon2Notices))
	for _, notice := range cfg.Argon2Notices {
		items = append(items, healthItem{
			Severity: healthWarn,
			Code:     interact.CodeArgon2Capped,
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
			Code:     interact.CodeConfigInvalid,
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

// checkBackupDirectoryHealth checks that the backup directory can be written.
// A missing directory is created by the first backup, unless the drive or
// share it is on is missing: that is an unreachable backup directory (e.g. a
// disconnected USB drive).
func checkBackupDirectoryHealth(backupDir string) []healthItem {
	info, err := os.Stat(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			if root := volumeRoot(backupDir); root != "" {
				if _, rootErr := os.Stat(root); rootErr != nil {
					return []healthItem{{
						Severity: healthError,
						Code:     interact.CodeBackupDirUnreachable,
						Scope:    healthScopeBackupDirectory,
						Detail:   fmt.Sprintf("%s → the drive or share %s is not available. Remedy: Connect the drive or check the network connection.", backupDir, root),
					}}
				}
			}
			return []healthItem{{
				Severity: healthWarn,
				Code:     interact.CodeBackupDirNew,
				Scope:    healthScopeBackupDirectory,
				Detail:   fmt.Sprintf("%s does not exist yet and will be created during backup", backupDir),
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Code:     interact.CodeBackupDirUnreachable,
			Scope:    healthScopeBackupDirectory,
			Detail:   fmt.Sprintf("%s → %v. Remedy: Check backup_directory in config.yaml and ensure read access.", backupDir, err),
		}}
	}

	if !info.IsDir() {
		return []healthItem{{
			Severity: healthError,
			Code:     interact.CodeBackupDirNotWritable,
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

// volumeRoot returns the root of the drive or share of path ("E:\",
// "\\server\share\"), or "" when path has none.
func volumeRoot(path string) string {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume == "" {
		return ""
	}
	return volume + string(filepath.Separator)
}

// probeWriteAccess creates and removes a temporary file in dir to confirm write
// and delete access. It returns health items using the given scope and remedy strings.
func probeWriteAccess(dir, scope, writeErrRemedy, cleanupErrRemedy string) []healthItem {
	display := filepath.ToSlash(dir)
	probe, err := os.CreateTemp(dir, ".restoresafe-health-*.tmp")
	if err != nil {
		return []healthItem{{
			Severity: healthError,
			Code:     interact.CodeBackupDirNotWritable,
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
			Code:     interact.CodeBackupDirNotWritable,
			Scope:    scope,
			Detail:   fmt.Sprintf("Temporary write probe cleanup failed: %v. Remedy: %s", err, cleanupErrRemedy),
		})
	}
	return items
}

// checkYubiKeyHealth reports whether the YubiKey the configuration needs is
// connected; connected is nil when no YubiKey is used.
func checkYubiKeyHealth(cfg *config.Config) (items []healthItem, connected *bool) {
	if !cfg.UseYubiKey() {
		return []healthItem{{
			Severity: healthOK,
			Scope:    healthScopeYubiKey,
			Detail:   "Disabled",
		}}, nil
	}

	ok := checkYubiKeyConnected() == nil
	if !ok {
		return []healthItem{{
			Severity: healthWarn,
			Code:     interact.CodeYubiKeyNotConnected,
			Scope:    healthScopeYubiKey,
			Detail:   "YubiKey not connected. Remedy: Connect the YubiKey before running backup, restore, or verify.",
		}}, &ok
	}
	return []healthItem{{
		Severity: healthOK,
		Scope:    healthScopeYubiKey,
		Detail:   "YubiKey connected",
	}}, &ok
}

func scanInventory(backupDir string) inventory {
	infos, err := catalog.Inventory(backupDir)
	inv := inventory{infos: infos, err: err}
	if err == nil {
		inv.legacy, _ = catalog.ListLegacyFiles(backupDir)
		inv.temps, _ = catalog.ListTempParts(backupDir)
	}
	return inv
}

func checkBackupInventoryHealth(cfg *config.Config, backupDir string) []healthItem {
	return inventoryItems(cfg, scanInventory(backupDir))
}

func inventoryItems(cfg *config.Config, inv inventory) []healthItem {
	if inv.err != nil {
		if os.IsNotExist(inv.err) {
			return []healthItem{{
				Severity: healthWarn,
				Code:     interact.CodeBackupDirNew,
				Scope:    healthScopeBackupInventory,
				Detail:   "Backup directory does not exist yet, no backups to inspect",
			}}
		}
		return []healthItem{{
			Severity: healthError,
			Code:     interact.CodeBackupDirUnreachable,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("Failed to scan backups: %v. Remedy: Check read permissions in backup directory.", inv.err),
		}}
	}

	items := make([]healthItem, 0)
	items = append(items, legacyAndTempItems(inv)...)

	if len(inv.infos) == 0 {
		items = append(items, healthItem{
			Severity: healthWarn,
			Code:     interact.CodeNoBackups,
			Scope:    healthScopeBackupInventory,
			Detail:   "No backup sets found. Remedy: Check backup directory or create a new backup run.",
		})
		return append(items, checkKeyHealth(cfg, inv.infos)...)
	}

	items = append(items, healthItem{
		Severity: healthOK,
		Scope:    healthScopeBackupInventory,
		Detail:   fmt.Sprintf("Found %d backup set(s)", len(inv.infos)),
	})
	items = append(items, buildBackupInventoryIssueItems(inv.infos)...)
	return append(items, checkKeyHealth(cfg, inv.infos)...)
}

// legacyAndTempItems warn about RestoreSafe 1.x backups (which 2.0 cannot
// restore and never touches) and about leftovers of interrupted backups
// (removed at the start of the next backup).
func legacyAndTempItems(inv inventory) []healthItem {
	var items []healthItem
	if len(inv.legacy) > 0 {
		items = append(items, healthItem{
			Severity: healthWarn,
			Code:     interact.CodeLegacyBackups,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("RestoreSafe 1.x backups found (%d file(s)). RestoreSafe 2.0 cannot restore them. Remedy: Keep RestoreSafe 1.0.2 to restore these files; they are never modified or deleted by 2.0.", len(inv.legacy)),
		})
	}
	if len(inv.temps) > 0 {
		items = append(items, healthItem{
			Severity: healthWarn,
			Code:     interact.CodeLeftoverTempFiles,
			Scope:    healthScopeBackupInventory,
			Detail:   fmt.Sprintf("%d leftover file(s) of an interrupted backup found (*.enc.tmp). They are removed at the start of the next backup.", len(inv.temps)),
		})
	}
	return items
}

func buildBackupInventoryIssueItems(infos []catalog.SetInfo) []healthItem {
	items := make([]healthItem, 0)
	structuralIssues := 0

	completeFulls := completeFullChains(infos)
	for _, info := range infos {
		if info.Err != nil {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Code:     interact.CodeSetIncomplete,
				Scope:    healthScopeBackupSet,
				Detail:   fmt.Sprintf("%s → %v", info.Entry.String(), info.Err),
			})
			continue
		}
		if info.Entry.IsDiff() && !completeFulls[info.Entry.ChainKey()] {
			structuralIssues++
			items = append(items, healthItem{
				Severity: healthError,
				Code:     interact.CodeBaseMissing,
				Scope:    healthScopeBackupSet,
				Detail:   baseMissingDetail(info),
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

// completeFullChains returns the chains (catalog ChainKey) that have a
// complete full backup.
func completeFullChains(infos []catalog.SetInfo) map[string]bool {
	fulls := make(map[string]bool)
	for _, info := range infos {
		if info.Complete() && !info.Entry.IsDiff() {
			fulls[info.Entry.ChainKey()] = true
		}
	}
	return fulls
}

func baseMissingDetail(info catalog.SetInfo) string {
	e := info.Entry
	return fmt.Sprintf("%s cannot be restored: the full backup [%s]_%s_*_FULL-*.enc of chain %s is missing or incomplete. Remedy: Restore the FULL files of %s from your copy, or delete the DIFF files of %s.", e.String(), e.DirectoryName, e.ChainID, e.ChainID, e.ChainID, e.ChainID)
}

// checkKeyHealth summarizes the current keys and whether the configuration
// requires new keys at the next backup.
func checkKeyHealth(cfg *config.Config, infos []catalog.SetInfo) []healthItem {
	ks := catalog.CurrentKeySet(infos)
	if ks == nil {
		return []healthItem{{
			Severity: healthOK,
			Scope:    healthScopeKeys,
			Detail:   "No keys yet; the next backup creates new keys",
		}}
	}
	detail := "Current keys " + ks.Summary()
	if reason := catalog.KeySetMismatch(cfg, ks); reason != "" {
		return []healthItem{{
			Severity: healthWarn,
			Code:     interact.CodeNewKeysNeeded,
			Scope:    healthScopeKeys,
			Detail:   fmt.Sprintf("%s. %s: the next backup creates new keys and full backups.", detail, reason),
		}}
	}
	return []healthItem{{Severity: healthOK, Scope: healthScopeKeys, Detail: detail}}
}
