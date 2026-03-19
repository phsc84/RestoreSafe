package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrSelectionCancelled indicates that the user intentionally cancelled selection.
var ErrSelectionCancelled = errors.New("selection cancelled")

// PromptBackupSelection asks the user to choose one or more backup entries.
func PromptBackupSelection(action, targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	for {
		if err := printBackupSelectionPrompt(action, targetDir, index); err != nil {
			return nil, "", err
		}

		selection, err := security.ReadLine("Selection: ")
		if err != nil {
			return nil, "", err
		}
		fmt.Println()
		selection = strings.TrimSpace(selection)
		if selection == "" {
			fmt.Println("Selection must not be empty. Remedy: Enter e.g. 'newest', a backup ID, or a full backup name.")
			fmt.Println()
			continue
		}

		var selected []util.BackupEntry

		switch strings.ToLower(selection) {
		case "q", "quit", "cancel":
			return nil, "", fmt.Errorf("%w. Remedy: Start %s again and enter a valid selection.", ErrSelectionCancelled, action)
		case "newest", "latest", "new":
			selected, label, err := catalog.ResolveNewestBackupRunSelection(targetDir, index)
			if err != nil {
				return nil, "", err
			}
			return selected, label, nil
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

func printBackupSelectionPrompt(action, targetDir string, index []util.BackupEntry) error {
	fmt.Println("Available backups:")
	runs, err := catalog.BackupRunSummaries(targetDir, index)
	if err != nil {
		return err
	}
	for _, run := range runs {
		fmt.Printf("  - Backup ID: %s / Timestamp (local): %s\n", run.ID, formatBackupRunTimestamp(run.NewestTime))
		for _, entry := range run.Entries {
			fmt.Printf("    - %s\n", entry.String())
		}
	}
	fmt.Println()

	completedAction := catalog.CompletedActionLabel(action)
	fmt.Printf("Select backup(s) to %s:\n", action)
	fmt.Printf("  - Enter newest -> newest backup set (all folders of the most recent run)\n")
	fmt.Printf("  - Enter backup ID only (e.g. ABC123) -> all folders with this ID will be %s\n", completedAction)
	fmt.Printf("  - Enter specific backup (e.g. MyFolder_2024-01-15_ABC123) -> only this folder will be %s\n", completedAction)
	fmt.Printf("  - Enter q -> cancel\n")
	fmt.Println()
	return nil
}

func formatBackupRunTimestamp(ts time.Time) string {
	return ts.Local().Format("2006-01-02 15:04:05 MST")
}
