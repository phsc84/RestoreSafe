// Package logging writes the log file of a backup run and mirrors its messages
// to the user, and reads the facts of a run back from its log (facts.go).
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/phsc84/restoresafe/internal/buildinfo"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/problem"
)

// Level represents the log verbosity.
type Level int

const (
	LevelInfo  Level = 0
	LevelDebug Level = 1
)

// Logger writes structured log entries to a log file and mirrors them to the
// user's output (the window's log pane); a logger without a file writes to the
// output only.
type Logger struct {
	level        Level
	file         *os.File
	originalPath string    // The path the user wanted (may be on network drive)
	actualPath   string    // The temp file this logger writes to; Close appends it to originalPath
	output       io.Writer // The user's output (nil: discarded)
	outputOnly   bool
	writeErr     error // the first line that could not be written to the file
	closed       bool
	mu           sync.Mutex
}

// FileError is a problem with the log file itself, which Close returns:
// lines that could not be written, or the run's lines left in the temp file
// because they could not be added to the log file.
type FileError struct {
	Err error
	// Kept is the temp file that holds the run's lines, "" when the log
	// file has them.
	Kept string
}

func (e *FileError) Error() string { return e.Err.Error() }

func (e *FileError) Unwrap() error { return e.Err }

func parseLogLevel(levelStr string) Level {
	if levelStr == "debug" {
		return LevelDebug
	}
	return LevelInfo
}

// orDiscard returns w, or io.Discard for nil.
func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// NewLogger creates a Logger writing to logPath and mirroring messages to
// output (nil: discarded).
// The lines go to a temp file of this logger's own, so writing never waits for
// a network drive; Close appends them to logPath. Two runs that log to the
// same file at once (a restore and a verification of one backup run) thus
// each add their own lines.
func NewLogger(logPath string, levelStr string, output io.Writer) (*Logger, error) {
	output = orDiscard(output)
	lvl := parseLogLevel(levelStr)

	_, statErr := os.Stat(logPath)
	logExists := statErr == nil

	f, err := os.CreateTemp("", "restoresafe-*-"+filepath.Base(logPath))
	if err != nil {
		return nil, problem.Errorf("Log file in temp directory could not be created: %w.", err).WithRemedy("Check TEMP/TMP path and write permissions.")
	}

	logger := &Logger{level: lvl, file: f, originalPath: logPath, actualPath: f.Name(), output: output}

	// Record the RestoreSafe version as the first line of a freshly created log
	// (file only, so it does not duplicate the startup banner in the output). Later
	// restore/verify runs reuse and append to the same log, leaving this first
	// line intact, so a backup's log identifies the version that created it.
	if !logExists {
		logger.InfoLogOnly("RestoreSafe v%s", buildinfo.Version)
	}

	return logger, nil
}

// NewOutputLogger creates a logger without a log file: it writes to output
// (nil: discarded) only.
func NewOutputLogger(levelStr string, output io.Writer) *Logger {
	return &Logger{level: parseLogLevel(levelStr), outputOnly: true, output: orDiscard(output)}
}

// HasFile reports whether the logger writes a log file; a nil logger does
// not.
func (l *Logger) HasFile() bool {
	if l == nil {
		return false
	}
	return !l.outputOnly
}

// Close flushes and closes the temp file, then appends its lines to the log
// file, which it creates if needed. It returns a *FileError when lines are
// missing from the log file: lines that could not be written, or all of
// them when the append failed; the temp file is then kept, as the only copy.
// Calls after the first return nil.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true

	if l.file != nil {
		// A failed sync shows in the read or the append below.
		_ = l.file.Sync()
		l.file.Close()
		l.file = nil
	}

	if l.actualPath != "" && l.originalPath != "" {
		data, err := os.ReadFile(l.actualPath)
		if err != nil {
			return &FileError{Err: problem.Errorf("The log in the temp directory could not be read: %w.", err).WithRemedy("Check the TEMP/TMP path and its read permissions."), Kept: l.actualPath}
		}
		if err := fsx.AppendFile(l.originalPath, data); err != nil {
			return &FileError{Err: problem.Errorf("The log could not be added to %s: %w.", filepath.ToSlash(l.originalPath), err).WithRemedy("Check the write permissions of the backup directory."), Kept: l.actualPath}
		}
		_ = os.Remove(l.actualPath)
	}
	if l.writeErr != nil {
		return &FileError{Err: problem.Errorf("Some lines could not be written to the log: %w.", l.writeErr).WithRemedy("Check the free space and write permissions of the TEMP/TMP directory.")}
	}
	return nil
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...any) {
	l.write(SeverityInfo, format, args...)
}

// Debug logs a debug message (only written at LevelDebug).
func (l *Logger) Debug(format string, args ...any) {
	if l.DebugEnabled() {
		l.write(SeverityDebug, format, args...)
	}
}

// DebugEnabled reports whether debug messages are written, so that callers
// can skip collecting what only a debug log shows.
func (l *Logger) DebugEnabled() bool {
	return l != nil && l.level >= LevelDebug
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...any) {
	l.write(SeverityWarn, format, args...)
}

// Error logs a failure: what the user asked for did not happen (a run, a
// set, a verification). A run that completed with something to look at
// logs warnings instead.
func (l *Logger) Error(format string, args ...any) {
	l.write(SeverityError, format, args...)
}

// InfoLogOnly logs an informational message without mirroring it to the output.
func (l *Logger) InfoLogOnly(format string, args ...any) {
	l.writeLogOnly(SeverityInfo, format, args...)
}

// WarnLogOnly logs a warning without mirroring it to the output.
func (l *Logger) WarnLogOnly(format string, args ...any) {
	l.writeLogOnly(SeverityWarn, format, args...)
}

func (l *Logger) write(severity, format string, args ...any) {
	l.writeLine(severity, true, format, args...)
}

func (l *Logger) writeLogOnly(severity, format string, args ...any) {
	l.writeLine(severity, false, format, args...)
}

func (l *Logger) writeLine(severity string, mirror bool, format string, args ...any) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	line := FormatLine(time.Now(), severity, fmt.Sprintf(format, args...))
	if mirror {
		// Write to the output first so the user sees messages immediately.
		fmt.Fprint(l.output, line)
	}
	// Also append to the log file and sync to ensure visibility.
	if l.file != nil {
		if _, err := l.file.WriteString(line); err != nil && l.writeErr == nil {
			l.writeErr = err
		}
	}
}
