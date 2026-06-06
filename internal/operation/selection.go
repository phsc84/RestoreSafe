package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrSelectionCancelled indicates that the user intentionally cancelled selection.
var ErrSelectionCancelled = errors.New("selection cancelled")

// PromptBackupSelection asks the user to choose one or more backup entries.
func PromptBackupSelection(action, backupDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	for {
		if err := printBackupSelectionPrompt(action, backupDir, index); err != nil {
			return nil, "", err
		}

		selection, err := readLineFn("Selection: ")
		if err != nil {
			return nil, "", err
		}
		fmt.Println()
		selection = strings.TrimSpace(selection)
		if selection == "" {
			fmt.Println("Selection must not be empty.")
			fmt.Println()
			continue
		}

		var selected []util.BackupEntry

		switch strings.ToLower(selection) {
		case "q":
			return nil, "", ErrSelectionCancelled
		case ".":
			selected, label, err := catalog.ResolveNewestBackupRunSelection(backupDir, index)
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

func printBackupSelectionPrompt(action, backupDir string, index []util.BackupEntry) error {
	fmt.Println("Available backups:")
	runs, err := catalog.BackupRunSummaries(backupDir, index)
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

	completedAction := completedActionLabel(action)
	fmt.Printf("Select backup(s) to %s:\n", action)
	fmt.Printf("  - Enter a dot (.) → newest backup set [backup ID %s]\n", runs[0].ID)
	fmt.Printf("  - Enter backup ID only (e.g. ABC123) → all directories with this ID will be %s\n", completedAction)
	fmt.Printf("  - Enter specific backup (e.g. MyDirectory_2024-01-15_ABC123) → only this directory will be %s\n", completedAction)
	fmt.Printf("  - Enter q → cancel\n")
	fmt.Println()
	return nil
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

func formatBackupRunTimestamp(ts time.Time) string {
	return ts.Local().Format("2006-01-02 15:04:05 MST")
}
