package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"sort"
	"strings"
	"time"
)

func promptRestoreSelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	return promptBackupSelection("restore", targetDir, index)
}

func promptBackupSelection(action, targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	runs := buildBackupRunSummaries(index)
	filterDate := ""

	for {
		visibleRuns := filterRunSummariesByDate(runs, filterDate)
		printBackupSelectionPrompt(action, targetDir, index, visibleRuns, filterDate)

		selection, err := security.ReadLine("Selection: ")
		if err != nil {
			return nil, "", err
		}
		selection = strings.TrimSpace(selection)
		if selection == "" {
			fmt.Println("Selection must not be empty. Remedy: Enter e.g. 'newest', a date (YYYY-MM-DD), a backup ID, or a full backup name.")
			fmt.Println()
			continue
		}

		var selected []util.BackupEntry

		switch strings.ToLower(selection) {
		case "q", "quit", "cancel":
			return nil, "", fmt.Errorf("Selection cancelled. Remedy: Start restore again and enter a valid selection.")
		case "all", "clear":
			filterDate = ""
			continue
		case "newest", "latest", "new":
			selected, label, err := resolveNewestBackupRunSelection(targetDir, index)
			if err != nil {
				return nil, "", err
			}
			return selected, label, nil
		}

		if isDateFilterInput(selection) {
			filtered := filterRunSummariesByDate(runs, selection)
			if len(filtered) == 0 {
				fmt.Printf("No backups found for date %s. Remedy: Use 'all' to clear the filter or 'newest' for the latest set.\n\n", selection)
				continue
			}
			filterDate = selection
			continue
		}

		normalized := strings.ToUpper(selection)
		if isRawBackupID(normalized) {
			selected, newestDate, allDates, found := resolveSelectionForIDNewestDate(normalized, index)
			if !found {
				fmt.Printf("Backup %q not found. Remedy: Check the ID in the list above or use 'newest'.\n\n", normalized)
				continue
			}

			if len(allDates) > 1 {
				fmt.Printf("Warning: Backup ID %s exists on multiple dates (%s). Using newest date %s. Remedy: Enter a full backup name if you want a specific date.\n\n", normalized, strings.Join(allDates, ", "), newestDate)
			}

			return selected, normalized, nil
		}

		selected, err = resolveSelection(selection, index)
		if err != nil {
			fmt.Printf("%v\n\n", err)
			continue
		}
		return selected, selection, nil
	}
}

type backupDateSummary struct {
	Date       string
	EntryCount int
	RunCount   int
}

type backupRunSummary struct {
	Date       string
	ID         util.BackupID
	Entries    []util.BackupEntry
	NewestTime time.Time
}

func buildBackupRunSummaries(index []util.BackupEntry) []backupRunSummary {
	runsByKey := make(map[string]backupRunSummary)
	for _, entry := range index {
		key := entry.Date + "|" + string(entry.ID)
		run := runsByKey[key]
		run.Date = entry.Date
		run.ID = entry.ID
		run.Entries = append(run.Entries, entry)
		runsByKey[key] = run
	}

	runs := make([]backupRunSummary, 0, len(runsByKey))
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

func formatRunFolderList(entries []util.BackupEntry) string {
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

func printBackupSelectionPrompt(action, targetDir string, index []util.BackupEntry, visibleRuns []backupRunSummary, filterDate string) {
	fmt.Println("Available backup sets:")
	if filterDate == "" {
		fmt.Println("  Date filter: all dates")
	} else {
		fmt.Printf("  Date filter: %s\n", filterDate)
	}
	if len(visibleRuns) == 0 {
		fmt.Println("  (no backup sets match the current filter)")
	} else {
		for _, run := range visibleRuns {
			fmt.Printf("  - %s / %s (%d folder(s): %s)\n", run.Date, run.ID, len(run.Entries), formatRunFolderList(run.Entries))
		}
	}
	fmt.Println()

	fmt.Println("Available dates:")
	for _, item := range sortedBackupDates(index) {
		marker := " "
		if item.Date == filterDate && filterDate != "" {
			marker = "*"
		}
		fmt.Printf("  %s %s (%d backup(s), %d set(s))\n", marker, item.Date, item.EntryCount, item.RunCount)
	}
	fmt.Println()

	if newest, err := newestBackupRunSummary(targetDir, index); err == nil {
		fmt.Printf("Newest set    : %s / %s (%d folder(s): %s)\n", newest.Date, newest.ID, len(newest.Entries), formatRunFolderList(newest.Entries))
	}

	completedAction := completedActionLabel(action)
	fmt.Printf("Select backup(s) to %s:\n", action)
	fmt.Printf("  - Enter a date (e.g. 2026-03-14) -> filter the shown backup sets\n")
	fmt.Printf("  - Enter all -> clear the date filter\n")
	fmt.Printf("  - Enter newest -> newest backup set (all folders of the most recent run)\n")
	fmt.Printf("  - Enter backup ID only (e.g. ABC123) -> all folders with this ID will be %s\n", completedAction)
	fmt.Printf("  - Enter specific backup (e.g. MyFolder_2024-01-15_ABC123) -> only this folder will be %s\n", completedAction)
	fmt.Printf("  - Enter q -> cancel\n")
	fmt.Println()
}

func sortedBackupDates(index []util.BackupEntry) []backupDateSummary {
	entryCounts := make(map[string]int)
	runKeysByDate := make(map[string]map[string]bool)
	for _, entry := range index {
		entryCounts[entry.Date]++
		if runKeysByDate[entry.Date] == nil {
			runKeysByDate[entry.Date] = make(map[string]bool)
		}
		runKeysByDate[entry.Date][string(entry.ID)] = true
	}

	items := make([]backupDateSummary, 0, len(entryCounts))
	for date, entryCount := range entryCounts {
		items = append(items, backupDateSummary{
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

func filterRunSummariesByDate(runs []backupRunSummary, date string) []backupRunSummary {
	if date == "" {
		filtered := make([]backupRunSummary, len(runs))
		copy(filtered, runs)
		return filtered
	}

	filtered := make([]backupRunSummary, 0)
	for _, run := range runs {
		if run.Date == date {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func isDateFilterInput(input string) bool {
	_, err := time.Parse("2006-01-02", strings.TrimSpace(input))
	return err == nil
}

func newestBackupRunSummary(targetDir string, index []util.BackupEntry) (backupRunSummary, error) {
	runsByKey := make(map[string]backupRunSummary)
	for _, entry := range index {
		newestTime, err := newestPartModTime(targetDir, entry)
		if err != nil {
			return backupRunSummary{}, fmt.Errorf("Failed to inspect newest backup set: %w. Remedy: Check whether all part files are readable.", err)
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
		return backupRunSummary{}, fmt.Errorf("No backups found. Remedy: Check whether .enc files are present in target_folder.")
	}

	runs := make([]backupRunSummary, 0, len(runsByKey))
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

func resolveNewestBackupRunSelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	run, err := newestBackupRunSummary(targetDir, index)
	if err != nil {
		return nil, "", err
	}
	return run.Entries, fmt.Sprintf("newest set %s/%s", run.Date, run.ID), nil
}

type backupIDDate struct {
	Date string
	ID   string
}

func sortedBackupIDDates(index []util.BackupEntry) []backupIDDate {
	seen := make(map[string]bool)
	items := make([]backupIDDate, 0)
	for _, e := range index {
		item := backupIDDate{Date: e.Date, ID: string(e.ID)}
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

// resolveSelection maps the user's input to one or more BackupEntry values.
func resolveSelection(input string, index []util.BackupEntry) ([]util.BackupEntry, error) {
	input = strings.TrimSpace(strings.ToUpper(input))

	if isRawBackupID(input) {
		matched, _, _, found := resolveSelectionForIDNewestDate(input, index)
		if found {
			return matched, nil
		}
	}

	// Try exact match on full name (case-insensitive).
	for _, e := range index {
		if strings.EqualFold(e.String(), input) {
			return []util.BackupEntry{e}, nil
		}
	}

	return nil, fmt.Errorf("Backup %q not found. Remedy: Use an ID from the list, 'newest', or a full backup name.", input)
}

func resolveSelectionForIDNewestDate(id string, index []util.BackupEntry) ([]util.BackupEntry, string, []string, bool) {
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

func isRawBackupID(input string) bool {
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

func completedActionLabel(action string) string {
	switch action {
	case "restore":
		return "restored"
	case "verify":
		return "verified"
	default:
		return action + "ed"
	}
}
