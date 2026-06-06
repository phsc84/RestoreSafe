package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var logFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})_([A-Z0-9]{6})\.log$`)

func applyRetentionPolicy(backupDir string, retentionKeep int, sources []backupSource, log *util.Logger) error {
	if retentionKeep <= 0 {
		log.Info("Cleanup old data disabled (retention_keep=%d)", retentionKeep)
		return nil
	}

	directorySet := make(map[string]bool)
	for _, source := range sources {
		if source.Err != nil {
			continue
		}
		backupName := source.BackupName
		if backupName == "" {
			backupName = util.DirectoryBaseName(source.Resolved)
		}
		directorySet[backupName] = true
	}
	if len(directorySet) == 0 {
		return nil
	}

	index, err := catalog.ScanBackups(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backups for retention: %w", err)
	}

	type datedEntry struct {
		entry      util.BackupEntry
		newestTime time.Time
	}

	entriesByDirectory := make(map[string][]datedEntry)
	for _, entry := range index {
		if !directorySet[entry.DirectoryName] {
			continue
		}
		newestTime, err := catalog.NewestPartModTime(backupDir, entry)
		if err != nil {
			log.Warn("Retention cleanup skipped: failed to inspect backup set %s (%v)", entry.String(), err)
			log.Warn("No retention cleanup was performed to avoid deleting backups based on incomplete metadata.")
			return nil
		}
		entriesByDirectory[entry.DirectoryName] = append(entriesByDirectory[entry.DirectoryName], datedEntry{entry: entry, newestTime: newestTime})
	}

	// The "Cleanup old data" header is logged lazily, immediately before the
	// first deletion, so retention stays silent when there is nothing to remove.
	headerShown := false
	showHeader := func() {
		if !headerShown {
			log.Info("Cleanup old data (retention: keeping %d)", retentionKeep)
			headerShown = true
		}
	}

	for _, entries := range entriesByDirectory {
		sort.Slice(entries, func(i, j int) bool {
			if !entries[i].newestTime.Equal(entries[j].newestTime) {
				return entries[i].newestTime.After(entries[j].newestTime)
			}
			if entries[i].entry.Date != entries[j].entry.Date {
				return entries[i].entry.Date > entries[j].entry.Date
			}
			return string(entries[i].entry.ID) > string(entries[j].entry.ID)
		})

		if len(entries) <= retentionKeep {
			continue
		}

		toDelete := entries[retentionKeep:]
		for _, candidate := range toDelete {
			deleted, err := deleteBackupEntryFiles(backupDir, candidate.entry)
			if err != nil {
				return fmt.Errorf("Failed to delete old backup set %s: %w. Remedy: Check delete permissions in the backup directory.", candidate.entry.String(), err)
			}
			for _, name := range deleted {
				showHeader()
				log.Info("  Deleted: %s", name)
			}
		}
	}

	deletedLogs, err := deleteOrphanLogFiles(backupDir)
	if err != nil {
		log.Warn("Retention log cleanup failed: %v", err)
	}
	for _, name := range deletedLogs {
		showHeader()
		log.Info("  Deleted: %s", name)
	}

	if !headerShown {
		log.Info("Cleanup old data (retention: keeping %d) - nothing to delete", retentionKeep)
	}

	return nil
}

// deleteBackupEntryFiles removes every part file plus the optional challenge
// file for a backup set, returning the base names of the files it deleted.
func deleteBackupEntryFiles(backupDir string, entry util.BackupEntry) ([]string, error) {
	var removed []string
	parts, err := catalog.CollectParts(backupDir, entry)
	if err != nil {
		return removed, err
	}
	for _, part := range parts {
		err := os.Remove(part)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		removed = append(removed, filepath.Base(part))
	}

	challengePath := util.ChallengeFileName(backupDir, entry.DirectoryName, entry.Date, entry.ID)
	if err := os.Remove(challengePath); err == nil {
		removed = append(removed, filepath.Base(challengePath))
	} else if !os.IsNotExist(err) {
		return removed, err
	}

	return removed, nil
}

// deleteOrphanLogFiles removes log files whose backup run no longer exists,
// returning the base names of the log files it deleted.
func deleteOrphanLogFiles(backupDir string) ([]string, error) {
	index, err := catalog.ScanBackups(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	activeRuns := make(map[string]bool)
	for _, entry := range index {
		activeRuns[entry.RunKey()] = true
	}

	des, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var deleted []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}

		matches := logFilePattern.FindStringSubmatch(de.Name())
		if matches == nil {
			continue
		}

		runKey := matches[1] + "|" + matches[2]
		if activeRuns[runKey] {
			continue
		}

		logPath := filepath.Join(backupDir, de.Name())
		err := os.Remove(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return deleted, err
		}
		deleted = append(deleted, de.Name())
	}

	return deleted, nil
}
