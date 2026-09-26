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

// PromptBackupSelection asks the user to choose one or more backup sets.
// runs must be newest first and non-empty.
func PromptBackupSelection(action string, runs []catalog.BackupRunSummary) ([]util.BackupEntry, error) {
	for {
		printBackupSelectionPrompt(action, runs)

		selection, err := readLineFn("Selection: ")
		if err != nil {
			return nil, err
		}
		fmt.Println()
		selection = strings.TrimSpace(selection)
		if selection == "" {
			fmt.Println("Selection must not be empty.")
			fmt.Println()
			continue
		}

		switch strings.ToLower(selection) {
		case "q":
			return nil, ErrSelectionCancelled
		case ".":
			return runs[0].Entries, nil
		}

		selected, err := catalog.ResolveSelection(selection, runs)
		if err != nil {
			fmt.Printf("%v Remedy: Check the ID or name in the list above.\n\n", err)
			continue
		}
		return selected, nil
	}
}

func printBackupSelectionPrompt(action string, runs []catalog.BackupRunSummary) {
	fmt.Println("Available backups:")
	for _, run := range runs {
		fmt.Printf("  - Backup ID: %s / Timestamp (local): %s\n", run.RunID, formatBackupRunTimestamp(run.Created))
		for _, entry := range run.Entries {
			fmt.Printf("    - %s\n", entry.String())
		}
	}
	fmt.Println()

	completedAction := completedActionLabel(action)
	fmt.Printf("Select backup(s) to %s:\n", action)
	fmt.Printf("  - Enter a dot (.) → newest backup run [backup ID %s]\n", runs[0].RunID)
	fmt.Printf("  - Enter backup ID only (e.g. ABC123) → all directories of this backup run will be %s\n", completedAction)
	fmt.Printf("  - Enter specific backup (e.g. MyDirectory_ABC123_2024-01-15_FULL) → only this directory will be %s\n", completedAction)
	fmt.Printf("  - Enter q → cancel\n")
	fmt.Println()
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
