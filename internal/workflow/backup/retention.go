package backup

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/problem"
	"RestoreSafe/internal/workflow/plan"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// applyRetentionPolicy deletes the backup sets that plan.Retention selects
// for the source directories, then the log files no longer needed. Files
// that do not follow the RestoreSafe 2 naming scheme (including 1.x backups)
// are never touched. Directories in hold are left untouched: their newest
// backup misses files that could not be read, which older backups may still
// contain. It records what it removed as a cleanup fact.
func applyRetentionPolicy(backupDir string, retentionKeep, keepDifferentials int, sources []plan.Source, hold map[string]bool, log *logging.Logger) error {
	if retentionKeep <= 0 && keepDifferentials <= 0 {
		log.Info("Cleanup old data disabled (retention_keep=%d, retention_keep_differentials=%d)", retentionKeep, keepDifferentials)
		return nil
	}
	if !anyUsableSource(sources) {
		return nil
	}

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backups for retention: %w", err)
	}
	removals, err := plan.Retention(sources, infos, retentionKeep, keepDifferentials)
	var unreadable *plan.UnreadableSetError
	if errors.As(err, &unreadable) {
		log.Warn("Retention cleanup skipped: %v", err)
		log.Warn("No retention cleanup was performed to avoid deleting backups based on incomplete metadata.")
		log.Fact(logging.Fact{Kind: logging.FactCleanup, Result: logging.ResultWarnings, Error: err.Error()})
		return nil
	}
	if err != nil {
		return err
	}

	// The "Cleanup old data" header is logged lazily, immediately before the
	// first deletion, so retention stays silent when there is nothing to remove.
	headerShown := false
	policy := retentionSummary(retentionKeep, keepDifferentials)
	logDeleted := func(names []string) {
		for _, name := range names {
			if !headerShown {
				log.Info("Cleanup old data (retention: %s)", policy)
				headerShown = true
			}
			log.Info("  Deleted: %s", name)
		}
	}

	directories := make([]string, 0, len(removals))
	for directory := range removals {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	removed := logging.Fact{Kind: logging.FactCleanup, Result: logging.ResultOK}
	for _, directory := range directories {
		if hold[directory] {
			log.Warn("Cleanup old data skipped for [%s]: the new backup misses files that could not be read, so older backups are kept.", directory)
			continue
		}
		for _, info := range removals[directory] {
			deleted, err := deleteSetFiles(info.Parts)
			logDeleted(deleted)
			if err != nil {
				err = problem.Errorf("Failed to delete old backup set %s: %w.", info.Entry.String(), err).WithRemedy("Check delete permissions in the backup directory.")
				removed.Result, removed.Error = logging.ResultFailed, err.Error()
				log.Fact(removed)
				return err
			}
			removed.Removed++
			removed.Bytes += info.SizeBytes
		}
	}

	deletedLogs, err := deleteOrphanLogFiles(backupDir)
	if err != nil {
		log.Warn("Retention log cleanup failed: %v", err)
	}
	logDeleted(deletedLogs)

	log.Fact(removed)

	if !headerShown {
		log.Info("Cleanup old data (retention: %s) - nothing to delete", policy)
	}
	return nil
}

// anyUsableSource reports whether at least one source directory can take part
// in retention.
func anyUsableSource(sources []plan.Source) bool {
	for _, src := range sources {
		if src.Err == nil {
			return true
		}
	}
	return false
}

// retentionSummary describes the retention settings, e.g.
// "keep 3 chain(s), all differentials".
func retentionSummary(retentionKeep, keepDifferentials int) string {
	chains := "all chains"
	if retentionKeep > 0 {
		chains = fmt.Sprintf("%d chain(s)", retentionKeep)
	}
	diffs := "all differentials"
	if keepDifferentials > 0 {
		diffs = fmt.Sprintf("%d differential(s) per chain", keepDifferentials)
	}
	return "keep " + chains + ", " + diffs
}

// deleteSetFiles removes the given part files and returns the base names of
// the files it deleted.
func deleteSetFiles(parts []string) ([]string, error) {
	var removed []string
	for _, part := range parts {
		if err := os.Remove(part); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		removed = append(removed, filepath.Base(part))
	}
	return removed, nil
}

// deleteOrphanLogFiles removes log files whose backup run no longer has any
// backup set, returning the base names of the log files it deleted. A log is
// kept while any remaining set was written by its run (header run ID) or
// carries its ID as chain ID, and 1.x logs next to 1.x backups are kept.
func deleteOrphanLogFiles(backupDir string) ([]string, error) {
	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	active := make(map[string]bool)
	for _, info := range infos {
		active[string(info.Entry.ChainID)] = true
		if info.Header != nil {
			active[info.Header.RunID] = true
		}
	}

	des, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	legacyLogs := make(map[string]bool)
	for _, de := range des {
		if name, ok := naming.LegacyLogFileName(de.Name()); ok {
			legacyLogs[name] = true
		}
	}

	var deleted []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		_, runID, isLog := naming.ParseLogFileName(de.Name())
		if !isLog || active[string(runID)] || legacyLogs[de.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(backupDir, de.Name())); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return deleted, err
		}
		deleted = append(deleted, de.Name())
	}
	return deleted, nil
}
