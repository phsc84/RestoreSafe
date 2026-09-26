package verify

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Run verifies selected backup sets without restoring them to disk: every
// part is decrypted and every file is checked against its manifest hash. u is
// asked for decisions and credentials.
func Run(u ui.UI, cfg *util.Config, exeDir string) error {
	out := u.Output()
	backupDir := util.ResolveDir(cfg.BackupDirectory, exeDir)

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory is readable.", backupDir, err)
	}
	runs := catalog.BackupRunSummaries(infos)
	if len(runs) == 0 {
		fmt.Fprintln(out, "No complete backups found in backup directory. Remedy: Check whether .enc files are in the backup directory and whether the correct directory is selected.")
		return nil
	}

	selected, err := u.SelectBackups("verify", runs)
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			fmt.Fprintln(out, "Verification cancelled.")
			return nil
		}
		return err
	}
	selectedInfos := catalog.SelectInfos(infos, selected)

	first := selectedInfos[0].Header
	logPath := util.LogFileName(backupDir, first.Date, util.BackupID(first.RunID))
	log := operation.OpenLogger(cfg, backupDir, first.Date, util.BackupID(first.RunID), out)
	warningCount := 0
	if log.IsConsoleOnly() {
		warningCount++
	}
	defer log.Close()

	preflight := buildVerifyPreflight(selectedInfos, infos)
	mode := util.AuthMode(first.KeySet.AuthMode)
	usesYubiKey := mode == util.AuthModePasswordYubiKey || mode == util.AuthModeYubiKey
	u.ShowReport(verifyPreflightReport(cfg, backupDir, preflight, usesYubiKey, mode == util.AuthModeYubiKey, security.CheckYubiKeyConnected))
	if err := validateVerifyPreflight(preflight); err != nil {
		return err
	}

	confirmed, err := u.ConfirmStart("verification")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Verification cancelled by user before start")
		fmt.Fprintln(out, "Verification cancelled.")
		return nil
	}

	masters, err := operation.UnlockKeySets(u, selectedInfos, "Enter verification password: ", log)
	if err != nil {
		return err
	}
	defer masters.Zero()

	return runVerifyOperation(out, selectedInfos, infos, backupDir, logPath, masters, log, warningCount)
}

// runVerifyOperation performs the verification using already-unlocked keys.
// It takes no further user input, so tests and automated flows can drive it
// directly by supplying the master keys. inventory is used to find the full
// backup of each selected differential.
func runVerifyOperation(out io.Writer, selected, inventory []catalog.SetInfo, backupDir, logPath string, masters operation.MasterKeys, log *util.Logger, warningCount int) error {
	fmt.Fprintln(out)
	first := selected[0].Header
	log.Info("Verification started - ID: %s, date: %s", first.RunID, first.Date)
	log.Info("Verification selection:")
	for _, info := range selected {
		log.Info("  %s", info.Entry.String())
	}

	skipped, err := verifySelectedEntries(selected, inventory, backupDir, masters, log)
	if err != nil {
		return err
	}
	if skipped > 0 {
		warningCount++
	}

	log.Info("Verification completed successfully.")
	fmt.Fprintf(out, "\nLog file: %s\n", logPath)
	if warningCount > 0 {
		fmt.Fprintf(out, "Warnings: %d\n", warningCount)
	}
	return nil
}

type verifyPreflightItem struct {
	Entry          util.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	// Base is the full backup a differential needs.
	Base *catalog.SetInfo
	Err  error
}

// buildVerifyPreflight checks the selected sets. Verifying a differential
// verifies the complete restore point, so its full backup is needed too.
func buildVerifyPreflight(selected, inventory []catalog.SetInfo) []verifyPreflightItem {
	items := make([]verifyPreflightItem, 0, len(selected))
	for _, info := range selected {
		item := verifyPreflightItem{
			Entry:          info.Entry,
			PartCount:      len(info.Parts),
			TotalSizeBytes: info.SizeBytes,
			Err:            info.Err,
		}
		if item.Err == nil && info.Entry.IsDiff() {
			base, err := catalog.BaseOf(inventory, info.Entry)
			if err != nil {
				item.Err = err
			} else {
				item.Base = base
				item.TotalSizeBytes += base.SizeBytes
			}
		}
		items = append(items, item)
	}
	return items
}

// verifyPreflightReport describes the verification: the selected backups
// (with the full backup a differential needs) and the issues that block it.
func verifyPreflightReport(
	cfg *util.Config,
	backupDir string,
	items []verifyPreflightItem,
	usesYubiKey, yubiKeyOnly bool,
	checkYubiKeyConnected func() error,
) ui.Report {
	var issues []ui.Issue
	rows := []ui.Row{ui.Heading("Backup selection"), ui.Item(ui.StatusNone, "Path: "+filepath.ToSlash(backupDir))}
	for _, item := range items {
		status := ui.StatusOK
		if item.Err != nil {
			status = ui.StatusError
			issues = append(issues, ui.Issue{Status: ui.StatusError, Text: item.Err.Error()})
		}
		var details []string
		if item.Base != nil {
			details = append(details, fmt.Sprintf("with full backup %s (parts: %d)", item.Base.Entry.String(), len(item.Base.Parts)))
		}
		rows = append(rows, ui.Item(status, fmt.Sprintf("%s (parts: %d)", item.Entry.String(), item.PartCount), details...))
	}
	rows = append(rows, operation.AuthRows(util.AuthModeFromFactors(usesYubiKey, yubiKeyOnly).Label(), usesYubiKey, "verification", checkYubiKeyConnected)...)

	size := "unknown"
	if totalBytes := estimateVerifyBytes(items); totalBytes > 0 {
		size = util.FormatBytesBinary(uint64(totalBytes))
	}
	summary := []ui.Row{ui.Field("Backup size", size), ui.Field("Log level", strings.ToLower(cfg.LogLevel))}

	return ui.Report{Title: "Verification preflight", Sections: []ui.Section{{Rows: rows}, {Rows: summary}}, Issues: issues}
}

func estimateVerifyBytes(items []verifyPreflightItem) int64 {
	var total int64
	for _, item := range items {
		if item.Err == nil {
			total += item.TotalSizeBytes
		}
	}
	return total
}

func validateVerifyPreflight(items []verifyPreflightItem) error {
	return operation.ValidatePreflightItems(
		items,
		func(item verifyPreflightItem) bool { return item.Err != nil },
		"Verification preflight failed: %d selected item(s) are incomplete or invalid. Remedy: Fix the [ERROR] entries above and start verification again.",
	)
}

// verifySelectedEntries verifies each selected set and returns the number of
// files missing from the restore points because they could not be read
// during backup.
func verifySelectedEntries(selected, inventory []catalog.SetInfo, backupDir string, masters operation.MasterKeys, log *util.Logger) (int, error) {
	skipped := 0
	for _, info := range selected {
		var base *util.BackupEntry
		if info.Entry.IsDiff() {
			baseInfo, err := catalog.BaseOf(inventory, info.Entry)
			if err != nil {
				return 0, err
			}
			base = &baseInfo.Entry
		}
		n, err := verifyEntry(info.Entry, base, backupDir, masters[info.Header.KeySet.ID], log)
		if err != nil {
			return 0, fmt.Errorf("Failed to verify directory %q: %w", info.Entry.String(), err)
		}
		skipped += n
	}
	return skipped, nil
}

// verifyEntry verifies one restore point: a full backup, or a differential
// together with its full backup base.
func verifyEntry(entry util.BackupEntry, base *util.BackupEntry, backupDir string, master []byte, log *util.Logger) (int, error) {
	set, err := catalog.OpenSet(backupDir, entry)
	if err != nil {
		return 0, err
	}
	defer set.Close()
	parts := len(set.Paths)
	var baseSet *container.Set
	if base != nil {
		baseSet, err = catalog.OpenSet(backupDir, *base)
		if err != nil {
			return 0, fmt.Errorf("Full backup %s: %w", base.String(), err)
		}
		defer baseSet.Close()
		parts += len(baseSet.Paths)
	}

	log.Info("Processing backup directory: %s", entry.DirectoryName)
	m, err := operation.ProcessRestorePoint(set, baseSet, master, "", true, log)
	if err != nil {
		return 0, err
	}
	log.Info("  Verified: %d file(s), %d directory(s) in %d part file(s) - [%s] successfully verified", m.Footer.Files, m.Footer.Dirs, parts, entry.DirectoryName)
	return operation.ReportSkippedFiles(m, entry.DirectoryName, log), nil
}
