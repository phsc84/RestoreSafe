package catalog

import (
	"RestoreSafe/internal/util"
	"fmt"
	"sort"
	"strings"
	"time"
)

type BackupDateSummary struct {
	Date       string
	EntryCount int
	RunCount   int
}

type BackupRunSummary struct {
	Date       string
	ID         util.BackupID
	Entries    []util.BackupEntry
	NewestTime time.Time
}

type BackupIDDate struct {
	Date string
	ID   string
}

func BuildBackupRunSummaries(index []util.BackupEntry) []BackupRunSummary {
	runsByKey := make(map[string]BackupRunSummary)
	for _, entry := range index {
		key := entry.Date + "|" + string(entry.ID)
		run := runsByKey[key]
		run.Date = entry.Date
		run.ID = entry.ID
		run.Entries = append(run.Entries, entry)
		runsByKey[key] = run
	}

	runs := make([]BackupRunSummary, 0, len(runsByKey))
	for _, run := range runsByKey {
		sort.Slice(run.Entries, func(i, j int) bool {
			return run.Entries[i].FolderName < run.Entries[j].FolderName
		})
		runs = append(runs, run)
	}

	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Date != runs[j].Date {
			return runs[i].Date > runs[j].Date
		}
		return string(runs[i].ID) > string(runs[j].ID)
	})

	return runs
}

func FormatRunFolderList(entries []util.BackupEntry) string {
	if len(entries) == 0 {
		return "none"
	}

	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.FolderName
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, %s, %s, +%d more", names[0], names[1], names[2], len(names)-3)
}

func SortedBackupDates(index []util.BackupEntry) []BackupDateSummary {
	entryCounts := make(map[string]int)
	runKeysByDate := make(map[string]map[string]bool)
	for _, entry := range index {
		entryCounts[entry.Date]++
		if runKeysByDate[entry.Date] == nil {
			runKeysByDate[entry.Date] = make(map[string]bool)
		}
		runKeysByDate[entry.Date][string(entry.ID)] = true
	}

	items := make([]BackupDateSummary, 0, len(entryCounts))
	for date, entryCount := range entryCounts {
		items = append(items, BackupDateSummary{
			Date:       date,
			EntryCount: entryCount,
			RunCount:   len(runKeysByDate[date]),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Date > items[j].Date
	})

	return items
}

func FilterRunSummariesByDate(runs []BackupRunSummary, date string) []BackupRunSummary {
	if date == "" {
		filtered := make([]BackupRunSummary, len(runs))
		copy(filtered, runs)
		return filtered
	}

	filtered := make([]BackupRunSummary, 0)
	for _, run := range runs {
		if run.Date == date {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func IsDateFilterInput(input string) bool {
	_, err := time.Parse("2006-01-02", strings.TrimSpace(input))
	return err == nil
}

func NewestBackupRunSummary(targetDir string, index []util.BackupEntry) (BackupRunSummary, error) {
	runsByKey := make(map[string]BackupRunSummary)
	for _, entry := range index {
		newestTime, err := NewestPartModTime(targetDir, entry)
		if err != nil {
			return BackupRunSummary{}, fmt.Errorf("Failed to inspect newest backup set: %w. Remedy: Check whether all part files are readable.", err)
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
		return BackupRunSummary{}, fmt.Errorf("No backups found. Remedy: Check whether .enc files are present in target_folder.")
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

	return runs[0], nil
}

func ResolveNewestBackupRunSelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	run, err := NewestBackupRunSummary(targetDir, index)
	if err != nil {
		return nil, "", err
	}
	return run.Entries, fmt.Sprintf("newest set %s/%s", run.Date, run.ID), nil
}

func SortedBackupIDDates(index []util.BackupEntry) []BackupIDDate {
	seen := make(map[string]bool)
	items := make([]BackupIDDate, 0)
	for _, e := range index {
		item := BackupIDDate{Date: e.Date, ID: string(e.ID)}
		key := item.Date + "|" + item.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date > items[j].Date
		}
		return items[i].ID < items[j].ID
	})

	return items
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
