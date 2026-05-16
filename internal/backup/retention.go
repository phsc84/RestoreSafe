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

func applyRetentionPolicy(targetDir string, retentionKeep int, sources []backupSource, log *util.Logger) error {
	if retentionKeep <= 0 {
		log.Info("Retention cleanup disabled (retention_keep=%d)", retentionKeep)
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

	index, err := catalog.ScanBackups(targetDir)
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
		newestTime, err := catalog.NewestPartModTime(targetDir, entry)
		if err != nil {
			log.Warn("Retention cleanup skipped: failed to inspect backup set %s (%v)", entry.String(), err)
			log.Warn("No retention cleanup was performed to avoid deleting backups based on incomplete metadata.")
			return nil
		}
		entriesByDirectory[entry.DirectoryName] = append(entriesByDirectory[entry.DirectoryName], datedEntry{entry: entry, newestTime: newestTime})
	}

	deletedSets := 0
	deletedFiles := 0

	for directoryName, entries := range entriesByDirectory {
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
			log.Info("Retention [%s]: %d backup set(s), nothing to delete (keep=%d)", directoryName, len(entries), retentionKeep)
			continue
		}

		toDelete := entries[retentionKeep:]
		for _, candidate := range toDelete {
			removed, err := deleteBackupEntryFiles(targetDir, candidate.entry)
			if err != nil {
				return fmt.Errorf("Failed to delete old backup set %s: %w. Remedy: Check delete permissions in the target directory.", candidate.entry.String(), err)
			}
			deletedSets++
			deletedFiles += removed
		}

		log.Info("Retention [%s]: deleted %d old backup set(s), keep=%d", directoryName, len(toDelete), retentionKeep)
	}

	deletedLogs, err := deleteOrphanLogFiles(targetDir)
	if err != nil {
		log.Warn("Retention log cleanup failed: %v", err)
	}

	log.Info("Retention cleanup finished: deleted %d backup set(s), %d backup file(s), %d orphan log file(s)", deletedSets, deletedFiles, deletedLogs)
	return nil
}

func deleteBackupEntryFiles(targetDir string, entry util.BackupEntry) (int, error) {
	removed := 0
	parts, err := catalog.CollectParts(targetDir, entry)
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
		removed++
	}

	challengePath := util.ChallengeFileName(targetDir, entry.DirectoryName, entry.Date, entry.ID)
	if err := os.Remove(challengePath); err == nil {
		removed++
	} else if !os.IsNotExist(err) {
		return removed, err
	}

	return removed, nil
}

func deleteOrphanLogFiles(targetDir string) (int, error) {
	index, err := catalog.ScanBackups(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	activeRuns := make(map[string]bool)
	for _, entry := range index {
		activeRuns[entry.RunKey()] = true
	}

	des, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	deleted := 0
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

		logPath := filepath.Join(targetDir, de.Name())
		err := os.Remove(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
}
