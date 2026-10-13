package view

import (
	"fmt"
	"strings"

	"github.com/phsc84/restoresafe/internal/logging"
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
		l, entry := logging.ParseLine(line)
		if entry && l.Severity == logging.SeverityFact {
			keep = false
			continue
		}
		tone := logTone(l.Severity)
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

// logTone returns the tone of a log line of severity.
func logTone(severity string) Tone {
	switch severity {
	case logging.SeverityWarn:
		return ToneWarning
	case logging.SeverityError:
		return ToneError
	}
	return ToneNeutral
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
