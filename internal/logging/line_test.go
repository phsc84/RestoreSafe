package logging_test

import (
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/logging"
)

func TestFormatLineAndParseLineRoundTrip(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 30, 9, 12, 3, 0, time.Local)
	for _, sev := range []string{logging.SeverityInfo, logging.SeverityDebug, logging.SeverityWarn, logging.SeverityFact, logging.SeverityError} {
		line := logging.FormatLine(at, sev, "a message - with a dash")
		if !strings.HasSuffix(line, "\n") || !strings.HasPrefix(line, "[2026-09-30 09:12:03] "+sev+" - ") {
			t.Fatalf("FormatLine: %q", line)
		}
		l, ok := logging.ParseLine(strings.TrimSuffix(line, "\n"))
		if !ok || !l.Time.Equal(at) || l.Severity != sev || l.Message != "a message - with a dash" {
			t.Fatalf("ParseLine(%q) = %+v, %v", line, l, ok)
		}
	}
	if l, ok := logging.ParseLine("[2026-09-30 09:12:03] INFO  - "); !ok || l.Message != "" {
		t.Fatalf("an empty message: %+v, %v", l, ok)
	}
}

func TestParseLineRefusesOtherText(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"",
		"  because it is in use",                      // continues the line before
		"[2026-09-30 09:12:03] INFO",                  // too short
		"(2026-09-30 09:12:03) INFO  - message",       // no brackets
		"[2026-09-30 09:12:03] INFO  : message",       // no separator
		"[2026-13-45 09:12:03] INFO  - message",       // no valid time
		"[2026-09-30 09:12:03 INFO  - message ......", // bracket missing
	} {
		if l, ok := logging.ParseLine(line); ok {
			t.Fatalf("ParseLine(%q) accepted it: %+v", line, l)
		}
	}
}
