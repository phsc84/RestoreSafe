package util

import "fmt"

// FormatBytesBinary formats bytes using 1024-based steps with user-friendly
// labels (KB, MB, GB, ...).
func FormatBytesBinary(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div := float64(unit)
	exp := 0
	for n := bytes / unit; n >= unit; n /= unit {
		exp++
		div *= unit
	}

	labels := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	if exp >= len(labels) {
		exp = len(labels) - 1
	}

	return fmt.Sprintf("%.2f %s", float64(bytes)/div, labels[exp])
}

// FormatInsufficientBackupSpaceMessage returns a consistent message for
// estimated backup size exceeding currently free target space.
func FormatInsufficientBackupSpaceMessage(neededBytes, availableBytes uint64) string {
	return fmt.Sprintf(
		"Insufficient free space for backup: needed %s, available %s. Remedy: Free disk space or choose a different target folder.",
		FormatBytesBinary(neededBytes),
		FormatBytesBinary(availableBytes),
	)
}

// FormatInsufficientRestoreSpaceMessage returns a consistent message for
// selected restore data exceeding currently free destination space.
func FormatInsufficientRestoreSpaceMessage(neededBytes, availableBytes uint64) string {
	return fmt.Sprintf(
		"Insufficient free space for restore: needed %s, available %s. Remedy: Free disk space or choose a different restore destination.",
		FormatBytesBinary(neededBytes),
		FormatBytesBinary(availableBytes),
	)
}
