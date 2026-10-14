package logging

import (
	"time"
)

// Severities of a log line, each five characters wide.
const (
	SeverityInfo  = "INFO "
	SeverityDebug = "DEBUG"
	SeverityWarn  = "WARN "
	SeverityFact  = "FACT "
	SeverityError = "ERROR"
)

// timestampLayout is the time format of every log line.
const timestampLayout = "2006-01-02 15:04:05"

// linePrefixLen is the length of "[2006-01-02 15:04:05] INFO  - ".
const linePrefixLen = len("[") + len(timestampLayout) + len("] ") + len(SeverityInfo) + len(" - ")

// FormatLine returns the log line of msg, written at t with severity:
// "[2006-01-02 15:04:05] WARN  - msg", ending in a newline. A msg with line
// breaks continues on lines of its own.
func FormatLine(t time.Time, severity, msg string) string {
	return "[" + t.Format(timestampLayout) + "] " + severity + " - " + msg + "\n"
}

// Line is a log line split into its parts.
type Line struct {
	// Time is when the line was written, in local time.
	Time     time.Time
	Severity string
	Message  string
}

// ParseLine splits a line that FormatLine wrote, without its newline. It
// returns false for any other text, such as a line that continues the
// message of the line before it.
func ParseLine(line string) (Line, bool) {
	if len(line) < linePrefixLen || line[0] != '[' || line[1+len(timestampLayout)] != ']' {
		return Line{}, false
	}
	sev := line[len("[")+len(timestampLayout)+len("] ") : linePrefixLen-len(" - ")]
	if line[linePrefixLen-len(" - "):linePrefixLen] != " - " {
		return Line{}, false
	}
	t, err := time.ParseInLocation(timestampLayout, line[1:1+len(timestampLayout)], time.Local)
	if err != nil {
		return Line{}, false
	}
	return Line{Time: t, Severity: sev, Message: line[linePrefixLen:]}, true
}
