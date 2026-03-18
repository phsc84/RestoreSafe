package catalog

import (
	"RestoreSafe/internal/util"
	"fmt"
	"sort"
	"strings"
	"time"
)

type BackupRunSummary struct {
	Date       string
	ID         util.BackupID
	Entries    []util.BackupEntry
	NewestTime time.Time
}

func BackupRunSummaries(targetDir string, index []util.BackupEntry) ([]BackupRunSummary, error) {
	runsByKey := make(map[string]BackupRunSummary)
	for _, entry := range index {
		newestTime, err := NewestPartModTime(targetDir, entry)
		if err != nil {
			return nil, fmt.Errorf("Failed to inspect backup sets: %w. Remedy: Check whether all part files are readable.", err)
		}

		key := entry.Date + "|" + string(entry.ID)
		run := runsByKey[key]
		run.Date = entry.Date
		run.ID = entry.ID
		run.Entries = append(run.Entries, entry)
		if newestTime.After(run.NewestTime) {
			run.NewestTime = newestTime
		}
		runsByKey[key] = run
	}

	if len(runsByKey) == 0 {
		return nil, fmt.Errorf("No backups found. Remedy: Check whether .enc files are present in target_folder.")
	}

	runs := make([]BackupRunSummary, 0, len(runsByKey))
	for _, run := range runsByKey {
		sort.Slice(run.Entries, func(i, j int) bool {
			return run.Entries[i].FolderName < run.Entries[j].FolderName
		})
		runs = append(runs, run)
	}

	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].NewestTime.Equal(runs[j].NewestTime) {
			return runs[i].NewestTime.After(runs[j].NewestTime)
		}
		if runs[i].Date != runs[j].Date {
			return runs[i].Date > runs[j].Date
		}
		return string(runs[i].ID) > string(runs[j].ID)
	})

	return runs, nil
}

func NewestBackupRunSummary(targetDir string, index []util.BackupEntry) (BackupRunSummary, error) {
	runs, err := BackupRunSummaries(targetDir, index)
	if err != nil {
		return BackupRunSummary{}, fmt.Errorf("Failed to inspect newest backup set: %w", err)
	}
	return runs[0], nil
}

func ResolveNewestBackupRunSelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	run, err := NewestBackupRunSummary(targetDir, index)
	if err != nil {
		return nil, "", err
	}
	return run.Entries, fmt.Sprintf("newest set %s/%s", run.Date, run.ID), nil
}

// ResolveSelection maps user input to one or more BackupEntry values.
func ResolveSelection(input string, index []util.BackupEntry) ([]util.BackupEntry, error) {
	input = strings.TrimSpace(strings.ToUpper(input))

	if IsRawBackupID(input) {
		matched, _, _, found := ResolveSelectionForIDNewestDate(input, index)
		if found {
			return matched, nil
		}
	}

	for _, e := range index {
		if strings.EqualFold(e.String(), input) {
			return []util.BackupEntry{e}, nil
		}
	}

	return nil, fmt.Errorf("Backup %q not found. Remedy: Use an ID from the list, 'newest', or a full backup name.", input)
}

func ResolveSelectionForIDNewestDate(id string, index []util.BackupEntry) ([]util.BackupEntry, string, []string, bool) {
	matchedByDate := make(map[string][]util.BackupEntry)
	for _, entry := range index {
		if string(entry.ID) != id {
			continue
		}
		matchedByDate[entry.Date] = append(matchedByDate[entry.Date], entry)
	}
	if len(matchedByDate) == 0 {
		return nil, "", nil, false
	}

	allDates := make([]string, 0, len(matchedByDate))
	for date := range matchedByDate {
		allDates = append(allDates, date)
	}
	sort.Slice(allDates, func(i, j int) bool {
		return allDates[i] > allDates[j]
	})

	newestDate := allDates[0]
	return matchedByDate[newestDate], newestDate, allDates, true
}

func IsRawBackupID(input string) bool {
	if len(input) != 6 {
		return false
	}
	for _, r := range input {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func CompletedActionLabel(action string) string {
	switch action {
	case "restore":
		return "restored"
	case "verify":
		return "verified"
	default:
		return action + "ed"
	}
}
