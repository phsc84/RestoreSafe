package engine

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// scanBackups walks targetDir and builds an index of all backup entries.
func scanBackups(targetDir string) ([]util.BackupEntry, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []util.BackupEntry

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		entry, _, ok := util.ParsePartFileName(de.Name())
		if !ok {
			continue
		}
		key := entry.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, entry)
		}
	}

	return result, nil
}

// collectParts returns the sorted part file paths for an entry.
func collectParts(targetDir string, entry util.BackupEntry) []string {
	des, err := os.ReadDir(targetDir)
	if err != nil {
		return nil
	}

	type seqPath struct {
		seq  int
		path string
	}
	var parts []seqPath

	for _, de := range des {
		e, seq, ok := util.ParsePartFileName(de.Name())
		if !ok {
			continue
		}
		if e.FolderName != entry.FolderName || e.Date != entry.Date || e.ID != entry.ID {
			continue
		}
		parts = append(parts, seqPath{seq, filepath.Join(targetDir, de.Name())})
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].seq < parts[j].seq })

	paths := make([]string, len(parts))
	for i, p := range parts {
		paths[i] = p.path
	}
	return paths
}

// sortedEntries returns index sorted by date desc, then folder name.
func sortedEntries(index []util.BackupEntry) []util.BackupEntry {
	sorted := make([]util.BackupEntry, len(index))
	copy(sorted, index)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Date != sorted[j].Date {
			return sorted[i].Date > sorted[j].Date
		}
		return sorted[i].FolderName < sorted[j].FolderName
	})
	return sorted
}

func backupRunUsesYubiKey(targetDir string, entry util.BackupEntry) (bool, error) {
	_, found, err := findChallengeFileForRun(targetDir, entry.Date, entry.ID)
	return found, err
}

func findChallengeFileForRun(targetDir, date string, id util.BackupID) (string, bool, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return "", false, err
	}

	suffix := fmt.Sprintf("_%s_%s.challenge", date, string(id))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), suffix) {
			return filepath.Join(targetDir, entry.Name()), true, nil
		}
	}

	return "", false, nil
}
