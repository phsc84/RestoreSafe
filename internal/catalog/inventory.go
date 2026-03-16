package catalog

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScanBackups walks targetDir and builds an index of all backup entries.
func ScanBackups(targetDir string) ([]util.BackupEntry, error) {
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

// CollectParts returns the sorted part file paths for an entry.
func CollectParts(targetDir string, entry util.BackupEntry) []string {
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

// SortedEntries returns entries sorted by date desc, then folder name.
func SortedEntries(index []util.BackupEntry) []util.BackupEntry {
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

// BackupRunUsesYubiKey checks whether a backup run has a matching challenge file.
// Returns (usesYubiKey, yubiKeyOnly, error).
// yubiKeyOnly is true when the backup was created without a password (YubiKey-only mode).
func BackupRunUsesYubiKey(targetDir string, entry util.BackupEntry) (bool, bool, error) {
	path, found, err := FindChallengeFileForRun(targetDir, entry.Date, entry.ID)
	if err != nil || !found {
		return found, false, err
	}
	yubiKeyOnly := IsChallengeFileYubiKeyOnly(path)
	return true, yubiKeyOnly, nil
}

// FindChallengeFileForRun returns the .challenge file path for date+ID if present.
func FindChallengeFileForRun(targetDir, date string, id util.BackupID) (string, bool, error) {
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

// IsChallengeFileYubiKeyOnly reports whether the challenge file was written
// for a YubiKey-only (no-password) backup by checking for the "NOPW:" prefix.
func IsChallengeFileYubiKeyOnly(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(data)), "NOPW:")
}

// NewestPartModTime returns the newest modification time among all part files.
func NewestPartModTime(targetDir string, entry util.BackupEntry) (time.Time, error) {
	parts := CollectParts(targetDir, entry)
	if len(parts) == 0 {
		return time.Time{}, fmt.Errorf("No part files found. Remedy: Ensure all .enc parts for this backup are present in target_folder.")
	}

	var newest time.Time
	for _, part := range parts {
		fi, err := os.Stat(part)
		if err != nil {
			return newest, err
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}

	return newest, nil
}
