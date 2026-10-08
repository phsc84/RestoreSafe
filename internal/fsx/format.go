package fsx

import (
	"fmt"

	"github.com/phsc84/restoresafe/internal/problem"
)

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

// InsufficientBackupSpace returns the error of a backup whose estimated size
// exceeds the free space of the backup directory.
func InsufficientBackupSpace(neededBytes, availableBytes uint64) error {
	return problem.Errorf("Insufficient free space for backup: needed %s, available %s.", FormatBytesBinary(neededBytes), FormatBytesBinary(availableBytes)).
		WithRemedy("Free disk space or choose a different backup directory.")
}

// InsufficientRestoreSpace returns the error of a restore whose data exceeds
// the free space of the destination.
func InsufficientRestoreSpace(neededBytes, availableBytes uint64) error {
	return problem.Errorf("Insufficient free space for restore: needed %s, available %s.", FormatBytesBinary(neededBytes), FormatBytesBinary(availableBytes)).
		WithRemedy("Free disk space or choose a different restore destination.")
}

// IsSpaceInsufficient reports whether the estimated byte count exceeds available free bytes.
// Returns false when estimatedBytes is zero or negative (unknown estimate).
func IsSpaceInsufficient(estimatedBytes int64, freeBytes uint64) bool {
	return estimatedBytes > 0 && uint64(estimatedBytes) > freeBytes
}
