package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/job"
)

// restorePreflightItem is a chosen set with the folder it is restored into.
type restorePreflightItem struct {
	job.SelectionItem
	OutputDir    string
	OutputDirErr error // output directory error (already exists, invalid name)
	// OutputDirCode classifies OutputDirErr.
	OutputDirCode interact.Code
}

// selections returns the chosen sets of items.
func selections(items []restorePreflightItem) []job.SelectionItem {
	out := make([]job.SelectionItem, len(items))
	for i, item := range items {
		out[i] = item.SelectionItem
	}
	return out
}

// buildRestorePreflight checks the selected sets (job.SelectionPreflight)
// and the folders they are restored into.
func buildRestorePreflight(selected, inventory []catalog.SetInfo, restorePath string) []restorePreflightItem {
	items := make([]restorePreflightItem, 0, len(selected))
	for _, sel := range job.SelectionPreflight(selected, inventory) {
		entry := sel.Entry
		item := restorePreflightItem{SelectionItem: sel, OutputDir: filepath.Join(restorePath, entry.DirectoryName)}
		if nameErr := naming.ValidateBackupEntryName(entry.DirectoryName); nameErr != nil {
			item.OutputDirErr, item.OutputDirCode = nameErr, interact.CodeRestoreTargetInvalid
		} else if _, err := os.Stat(item.OutputDir); err == nil {
			item.OutputDirErr, item.OutputDirCode = problem.New("Restore directory already exists.").WithRemedy("Choose a different restore destination or rename/delete the existing restore directory."), interact.CodeRestoreTargetExists
		}
		items = append(items, item)
	}
	return items
}

// restorePreflightReport describes the restore: the selected backups (with
// the full backup a differential needs), the destination, and the directories
// to be created, with the issues that block the restore.
func restorePreflightReport(
	cfg *config.Config,
	backupDir, restorePath string,
	items []restorePreflightItem,
	mode config.AuthMode,
	checkYubiKeyConnected func() error,
) interact.Report {
	rows, issues := job.SelectionRows(backupDir, selections(items))
	addError := func(code interact.Code, err error) {
		issues = append(issues, interact.IssueOf(interact.StatusError, code, err))
	}

	estimatedRestoreBytes := estimateRestoreBytes(items)
	destDisplay := displayRestoreOutputDir(restorePath)
	restoreFreeBytes, restoreFreeErr := queryRestoreTargetFreeBytes(restorePath)

	rows = append(rows, interact.Heading("Restore destination"))
	if restoreFreeErr != nil {
		rows = append(rows, interact.Item(interact.StatusError, destDisplay))
		addError(interact.CodeFreeSpaceUnknown, fmt.Errorf("Cannot query free space for restore destination %s: %v", destDisplay, restoreFreeErr))
	} else {
		rows = append(rows, interact.Item(interact.StatusOK, destDisplay))
		if fsx.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
			addError(interact.CodeSpaceInsufficient, fsx.InsufficientRestoreSpace(uint64(estimatedRestoreBytes), restoreFreeBytes))
		}
	}

	rows = append(rows, interact.Heading("Restored directory(s)"))
	for _, item := range items {
		status := interact.StatusOK
		if item.OutputDirErr != nil {
			status = interact.StatusError
			addError(item.OutputDirCode, item.OutputDirErr)
		}
		rows = append(rows, interact.Item(status, displayRestoreOutputDir(item.OutputDir)))
	}
	rows = append(rows, job.AuthRows(mode.Label(), mode.UsesYubiKey(), "restore", checkYubiKeyConnected)...)

	summary := []interact.Row{interact.Field("Backup size", "unknown")}
	if estimatedRestoreBytes > 0 {
		summary[0].Text = fsx.FormatBytesBinary(uint64(estimatedRestoreBytes))
	}
	if restoreFreeErr != nil {
		summary = append(summary, interact.Field("Free space", fmt.Sprintf("unknown (%v)", restoreFreeErr)))
	} else {
		summary = append(summary, interact.Field("Free space", fsx.FormatBytesBinary(restoreFreeBytes)))
	}
	summary = append(summary, interact.Field("Log level", strings.ToLower(cfg.LogLevel)))

	return interact.Report{Title: "Restore preflight", Sections: []interact.Section{{Rows: rows}, {Rows: summary}}, Issues: issues}
}

func displayRestoreOutputDir(outputDir string) string {
	absoluteOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		absoluteOutputDir = filepath.Clean(outputDir)
	}
	return filepath.ToSlash(absoluteOutputDir)
}

func validateRestorePreflight(items []restorePreflightItem) error {
	return job.ValidatePreflightItems(
		items,
		func(item restorePreflightItem) bool { return item.Err != nil || item.OutputDirErr != nil },
		"Restore preflight failed: %d selected item(s) are invalid.",
		"Fix the [ERROR] entries above and start restore again.",
	)
}

func validateRestoreTargetSpace(restorePath string, items []restorePreflightItem) error {
	estimatedRestoreBytes := estimateRestoreBytes(items)
	if estimatedRestoreBytes <= 0 {
		return nil
	}

	restoreFreeBytes, err := queryRestoreTargetFreeBytes(restorePath)
	if err != nil {
		return nil
	}

	if !fsx.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
		return nil
	}

	return fmt.Errorf("Restore preflight failed: %w", fsx.InsufficientRestoreSpace(uint64(estimatedRestoreBytes), restoreFreeBytes))
}

func estimateRestoreBytes(items []restorePreflightItem) int64 {
	return job.SelectionBytes(selections(items))
}

func queryRestoreTargetFreeBytes(restorePath string) (uint64, error) {
	probe := filepath.Clean(restorePath)
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if info.IsDir() {
				return fsx.QueryFreeSpaceBytes(probe)
			}
			probe = filepath.Dir(probe)
		} else {
			parent := filepath.Dir(probe)
			if parent == probe {
				break
			}
			probe = parent
		}
	}

	return fsx.QueryFreeSpaceBytes(restorePath)
}
