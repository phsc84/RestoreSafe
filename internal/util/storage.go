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
