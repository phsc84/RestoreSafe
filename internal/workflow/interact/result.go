package interact

// Result is the outcome of an operation that completed.
type Result struct {
	// Warnings counts the warnings of the run; the output and the log file
	// describe them.
	Warnings int
	// LogPath is the run's log file.
	LogPath string
}

// LogFailureKind says what went wrong with the log file of an operation.
type LogFailureKind int

const (
	// LogNotOpened: the operation runs without a log file.
	LogNotOpened LogFailureKind = iota + 1
	// LogIncomplete: some lines could not be written; the log in the backup
	// directory has the others.
	LogIncomplete
	// LogKept: the run's lines could not be added to the log in the backup
	// directory; they are in a copy in TEMP.
	LogKept
)

// LogFailure is a problem with the log file itself, which the log can't
// record (UI.LogFailed).
type LogFailure struct {
	Kind LogFailureKind
	// Err says what failed.
	Err error
	// Kept is the copy in TEMP that holds the run's lines (LogKept).
	Kept string
}
