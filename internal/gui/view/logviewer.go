package view

import (
	"fmt"
	"strings"
)

// LogLine is a line of the log pane.
type LogLine struct {
	Text string
	Tone Tone
}

// LogFilter chooses the lines of the log pane.
type LogFilter int

const (
	LogAll LogFilter = iota
	LogWarnings
)

// LogLinesOf returns the lines of the log text to show (GUI spec BK-5): as the
// file stores them, without the machine-readable fact lines; WARN and
// ERROR lines are marked. LogWarnings keeps only those, with the lines
// that continue them; when there are none, a line says so.
func LogLinesOf(text string, filter LogFilter) []LogLine {
	var out []LogLine
	keep := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		tone, entry := logTone(line)
		if strings.Contains(line, "] FACT  - ") {
			keep = false
			continue
		}
		if entry {
			keep = tone == ToneWarning || tone == ToneError
		} else if len(out) > 0 {
			tone = out[len(out)-1].Tone
		}
		if filter == LogWarnings && !keep {
			continue
		}
		out = append(out, LogLine{Text: line, Tone: tone})
	}
	if filter == LogWarnings && len(out) == 0 && strings.TrimSpace(text) != "" {
		out = append(out, LogLine{Text: logNoWarnings, Tone: ToneSecondary})
	}
	return out
}

// logTone returns the tone of a log line and whether it starts an entry
// ("[2026-09-30 09:12:03] WARN  - ...").
func logTone(line string) (Tone, bool) {
	if !strings.HasPrefix(line, "[") || len(line) < 22 || line[20] != ']' {
		return ToneNeutral, false
	}
	switch rest := line[21:]; {
	case strings.HasPrefix(rest, " WARN"):
		return ToneWarning, true
	case strings.HasPrefix(rest, " ERROR"):
		return ToneError, true
	}
	return ToneNeutral, true
}

// LogWindowTitle titles the log window: "Log of today, 09:12
// (2026-09-30_QRS321.log)", or "Log (2026-09-30_QRS321.log)" without the
// run's date.
func LogWindowTitle(when, file string) string {
	if when == "" {
		return fmt.Sprintf(logTitleFile, file)
	}
	return fmt.Sprintf(logTitleOf, when, file)
}

// LogViewer are the labels of the log window's buttons.
type LogViewer struct {
	All, Warnings, Open string
}

// LogViewerOf returns the labels of the log window.
func LogViewerOf() LogViewer {
	return LogViewer{All: logFilterAll, Warnings: logFilterWarnings, Open: buttonOpenLog}
}
