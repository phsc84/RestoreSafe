package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"strings"
)

// PromptBackupSelection asks the user to choose one or more backup entries.
func PromptBackupSelection(action, targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	runs := catalog.BuildBackupRunSummaries(index)
	filterDate := ""

	for {
		visibleRuns := catalog.FilterRunSummariesByDate(runs, filterDate)
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
			selected, label, err := catalog.ResolveNewestBackupRunSelection(targetDir, index)
			if err != nil {
				return nil, "", err
			}
			return selected, label, nil
		}

		if catalog.IsDateFilterInput(selection) {
			filtered := catalog.FilterRunSummariesByDate(runs, selection)
			if len(filtered) == 0 {
				fmt.Printf("No backups found for date %s. Remedy: Use 'all' to clear the filter or 'newest' for the latest set.\n\n", selection)
				continue
			}
			filterDate = selection
			continue
		}

		normalized := strings.ToUpper(selection)
		if catalog.IsRawBackupID(normalized) {
			selected, newestDate, allDates, found := catalog.ResolveSelectionForIDNewestDate(normalized, index)
			if !found {
				fmt.Printf("Backup %q not found. Remedy: Check the ID in the list above or use 'newest'.\n\n", normalized)
				continue
			}

			if len(allDates) > 1 {
				fmt.Printf("Warning: Backup ID %s exists on multiple dates (%s). Using newest date %s. Remedy: Enter a full backup name if you want a specific date.\n\n", normalized, strings.Join(allDates, ", "), newestDate)
			}

			return selected, normalized, nil
		}

		selected, err = catalog.ResolveSelection(selection, index)
		if err != nil {
			fmt.Printf("%v\n\n", err)
			continue
		}
		return selected, selection, nil
	}
}

func printBackupSelectionPrompt(action, targetDir string, index []util.BackupEntry, visibleRuns []catalog.BackupRunSummary, filterDate string) {
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
			fmt.Printf("  - %s / %s (%d folder(s): %s)\n", run.Date, run.ID, len(run.Entries), catalog.FormatRunFolderList(run.Entries))
		}
	}
	fmt.Println()

	fmt.Println("Available dates:")
	for _, item := range catalog.SortedBackupDates(index) {
		marker := " "
		if item.Date == filterDate && filterDate != "" {
			marker = "*"
		}
		fmt.Printf("  %s %s (%d backup(s), %d set(s))\n", marker, item.Date, item.EntryCount, item.RunCount)
	}
	fmt.Println()

	if newest, err := catalog.NewestBackupRunSummary(targetDir, index); err == nil {
		fmt.Printf("Newest set    : %s / %s (%d folder(s): %s)\n", newest.Date, newest.ID, len(newest.Entries), catalog.FormatRunFolderList(newest.Entries))
	}

	completedAction := catalog.CompletedActionLabel(action)
	fmt.Printf("Select backup(s) to %s:\n", action)
	fmt.Printf("  - Enter a date (e.g. 2026-03-14) -> filter the shown backup sets\n")
	fmt.Printf("  - Enter all -> clear the date filter\n")
	fmt.Printf("  - Enter newest -> newest backup set (all folders of the most recent run)\n")
	fmt.Printf("  - Enter backup ID only (e.g. ABC123) -> all folders with this ID will be %s\n", completedAction)
	fmt.Printf("  - Enter specific backup (e.g. MyFolder_2024-01-15_ABC123) -> only this folder will be %s\n", completedAction)
	fmt.Printf("  - Enter q -> cancel\n")
	fmt.Println()
}
