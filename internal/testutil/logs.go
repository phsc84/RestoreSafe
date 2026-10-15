package testutil

import (
	"os"
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
