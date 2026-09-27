package ui

// Result is the outcome of an operation that completed.
type Result struct {
	// Warnings counts the warnings of the run; the output and the log file
	// describe them.
	Warnings int
	// LogPath is the run's log file.
	LogPath string
}
