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

// chain groups the backup sets of one directory that share a chain ID.
type chain struct {
	id          util.BackupID
	fullCreated time.Time
	hasFull     bool
	sets        []catalog.SetInfo
}

// applyRetentionPolicy keeps the newest retentionKeep chains (a full backup
// plus its differentials) per configured source directory and deletes older
// chains as a whole, so a full backup is never deleted while a kept
// differential depends on it. Incomplete sets older than the newest complete
// set of their directory are deleted too. Files that do not follow the
// RestoreSafe 2 naming scheme (including 1.x backups) are never touched.
// Directories in hold are left untouched: their newest backup misses files
// that could not be read, which older backups may still contain.
func applyRetentionPolicy(backupDir string, retentionKeep int, sources []backupSource, hold map[string]bool, log *util.Logger) error {
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

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backups for retention: %w", err)
	}
	for _, info := range infos {
		if directorySet[info.Entry.DirectoryName] && info.Err != nil && !catalog.IsIncomplete(info.Err) {
			log.Warn("Retention cleanup skipped: failed to inspect backup set %s (%v)", info.Entry.String(), info.Err)
			log.Warn("No retention cleanup was performed to avoid deleting backups based on incomplete metadata.")
			return nil
		}
	}

	// The "Cleanup old data" header is logged lazily, immediately before the
	// first deletion, so retention stays silent when there is nothing to remove.
	headerShown := false
	logDeleted := func(names []string) {
		for _, name := range names {
			if !headerShown {
				log.Info("Cleanup old data (retention: keeping %d)", retentionKeep)
				headerShown = true
			}
			log.Info("  Deleted: %s", name)
		}
	}

	for directory := range directorySet {
		if hold[directory] {
			log.Warn("Cleanup old data skipped for [%s]: the new backup misses files that could not be read, so older backups are kept.", directory)
			continue
		}
		toDelete := retentionCandidates(directory, infos, retentionKeep)
		for _, info := range toDelete {
			deleted, err := deleteSetFiles(info.Parts)
			logDeleted(deleted)
			if err != nil {
				return fmt.Errorf("Failed to delete old backup set %s: %w. Remedy: Check delete permissions in the backup directory.", info.Entry.String(), err)
			}
		}
	}

	deletedLogs, err := deleteOrphanLogFiles(backupDir)
	if err != nil {
		log.Warn("Retention log cleanup failed: %v", err)
	}
	logDeleted(deletedLogs)

	if !headerShown {
		log.Info("Cleanup old data (retention: keeping %d) - nothing to delete", retentionKeep)
	}
	return nil
}

// retentionCandidates returns the sets of directory that retention deletes.
func retentionCandidates(directory string, infos []catalog.SetInfo, retentionKeep int) []catalog.SetInfo {
	chains := make(map[util.BackupID]*chain)
	var incomplete []catalog.SetInfo
	var newestComplete time.Time
	for _, info := range infos {
		if info.Entry.DirectoryName != directory {
			continue
		}
		if !info.Complete() {
			incomplete = append(incomplete, info)
			continue
		}
		if c := info.Created(); c.After(newestComplete) {
			newestComplete = c
		}
		ch := chains[info.Entry.ChainID]
		if ch == nil {
			ch = &chain{id: info.Entry.ChainID}
			chains[info.Entry.ChainID] = ch
		}
		ch.sets = append(ch.sets, info)
		if !info.Entry.IsDiff() {
			ch.hasFull = true
			ch.fullCreated = info.Created()
		}
	}

	// Only chains with a complete full backup take part in retention; a chain
	// whose full backup is missing is reported by the health check instead.
	ordered := make([]*chain, 0, len(chains))
	for _, ch := range chains {
		if ch.hasFull {
			ordered = append(ordered, ch)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].fullCreated.Equal(ordered[j].fullCreated) {
			return ordered[i].fullCreated.After(ordered[j].fullCreated)
		}
		return ordered[i].id > ordered[j].id
	})

	var out []catalog.SetInfo
	if len(ordered) > retentionKeep {
		for _, ch := range ordered[retentionKeep:] {
			out = append(out, ch.sets...)
		}
	}
	for _, info := range incomplete {
		if !newestComplete.IsZero() && newestPartModTime(info.Parts).Before(newestComplete) {
			out = append(out, info)
		}
	}
	return out
}

func newestPartModTime(parts []string) time.Time {
	var newest time.Time
	for _, p := range parts {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
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
		if name, ok := util.LegacyLogFileName(de.Name()); ok {
			legacyLogs[name] = true
		}
	}

	var deleted []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		matches := logFilePattern.FindStringSubmatch(de.Name())
		if matches == nil || active[matches[2]] || legacyLogs[de.Name()] {
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
