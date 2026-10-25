package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/logging"
)

// ErrorLines returns the messages of the ERROR lines in the log text.
func ErrorLines(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l, ok := logging.ParseLine(line); ok && l.Severity == logging.SeverityError {
			out = append(out, l.Message)
		}
	}
	return out
}

// LogErrors returns the messages of the ERROR lines in the log file at path.
func LogErrors(t testing.TB, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return ErrorLines(string(data))
}

// NewLog returns a logger at level that writes a log file of its own, and a
// function that closes the logger and returns the lines of that file.
func NewLog(t testing.TB, level string) (*logging.Logger, func() string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.log")
	log, err := logging.NewLogger(path, level)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	return log, func() string {
		t.Helper()
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(data)
	}
}
