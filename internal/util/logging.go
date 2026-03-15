package util

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level represents the log verbosity.
type Level int

const (
	LevelInfo  Level = 0
	LevelDebug Level = 1
)

// Logger writes structured log entries to a file and optionally to stdout.
type Logger struct {
	level        Level
	file         *os.File
	originalPath string // The path the user wanted (may be on network drive)
	actualPath   string // The path we actually write to (may be temp fallback)
	mu           sync.Mutex
}

// NewLogger creates a Logger writing to logPath. stdoutToo controls console mirroring.
// Strategy: Always write to a temp file to avoid network write issues.
// If a log for this backup ID already exists, copy it to temp first (for appending).
// On Close(), copy the complete temp log back to the original path.
func NewLogger(logPath string, levelStr string) (*Logger, error) {
	lvl := LevelInfo
	if levelStr == "debug" {
		lvl = LevelDebug
	}

	// Determine temp path for actual writes.
	base := filepath.Base(logPath)
	tempPath := filepath.Join(os.TempDir(), base)

	// If the original log file already exists, copy it to temp first (for appending).
	if _, err := os.Stat(logPath); err == nil {
		// File exists; copy it to temp.
		data, err := os.ReadFile(logPath)
		if err != nil {
			// Warn but continue; we'll create a new log in temp.
			fmt.Fprintf(os.Stderr, "Warning: Existing log file could not be read: %v. Remedy: Check read permissions for the target log file.\n", err)
		} else {
			// Write the existing content to the temp file.
			if err := os.WriteFile(tempPath, data, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Existing log file could not be copied to the temp directory: %v. Remedy: Check write permissions for TEMP/TMP.\n", err)
			}
		}
	}

	// Open temp log for appending.
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("Log file in temp directory could not be created: %w. Remedy: Check TEMP/TMP path and write permissions.", err)
	}

	return &Logger{level: lvl, file: f, originalPath: logPath, actualPath: tempPath}, nil
}

// Close flushes and closes the underlying log file, then copies it from temp
// back to the original path on the network drive.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		if err := l.file.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Syncing log file before close failed: %v. Remedy: Retry; if it persists, check file-system health.\n", err)
		}
		l.file.Close()
		l.file = nil
	}

	// Copy the complete temp log back to the original path.
	if l.actualPath != "" && l.originalPath != "" && l.actualPath != l.originalPath {
		data, err := os.ReadFile(l.actualPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading log file in temp directory: %v. Remedy: Check TEMP/TMP path and read permissions.\n", err)
			return
		}
		// Overwrite the original file with the complete temp log content.
		if err := os.WriteFile(l.originalPath, data, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing log file to target directory: %v. Remedy: Check target-folder write permissions.\n", err)
			fmt.Fprintf(os.Stderr, "Log file is located in temp directory: %s\n", l.actualPath)
			return
		}
		// Cleanup temp file.
		_ = os.Remove(l.actualPath)
		fmt.Fprintf(os.Stderr, "Log file successfully copied to: %s\n", l.originalPath)
	}
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...any) {
	l.write("INFO ", format, args...)
}

// Debug logs a debug message (only written at LevelDebug).
func (l *Logger) Debug(format string, args ...any) {
	if l.level >= LevelDebug {
		l.write("DEBUG", format, args...)
	}
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...any) {
	l.write("WARN ", format, args...)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...any) {
	l.write("ERROR", format, args...)
}

func (l *Logger) write(severity, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s - %s\n", ts, severity, msg)
	// Write to stdout first so interactive users see messages immediately.
	fmt.Fprint(os.Stdout, line)
	// Also append to the log file and sync to ensure visibility.
	if l.file != nil {
		if _, err := l.file.WriteString(line); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Writing to log file failed: %v. Remedy: Check write permissions and free disk space.\n", err)
			return
		}
	}
}
