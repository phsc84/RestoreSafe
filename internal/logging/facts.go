package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Fact is a machine-readable result of an operation. It is written to the
// log file as one line (never to the user's output), so the results of a
// backup run stay with its backup sets and are deleted with them.
type Fact struct {
	// Kind is FactBackup, FactSet or FactVerify.
	Kind string `json:"kind"`
	// Result is ResultOK, ResultWarnings, ResultFailed or ResultCancelled.
	Result string `json:"result"`
	// Set is the backup set a set or verify fact is about.
	Set string `json:"set,omitempty"`
	// Warnings counts the warnings of a backup run.
	Warnings int `json:"warnings,omitempty"`
	// Skipped counts the files a backup set misses because they could not
	// be read.
	Skipped int `json:"skipped,omitempty"`
	// Seconds is the duration of a backup run.
	Seconds int64 `json:"seconds,omitempty"`
	// Error is the reason of a failure.
	Error string `json:"error,omitempty"`
	// Time is when the fact was written, from the log line.
	Time time.Time `json:"-"`
}

// Fact kinds and results.
const (
	FactBackup = "backup"
	FactVerify = "verify"
	FactSet    = "set"

	ResultOK        = "ok"
	ResultWarnings  = "warnings"
	ResultFailed    = "failed"
	ResultCancelled = "cancelled"
)

// factSeverity marks fact lines; it has the width of the other severities.
const factSeverity = "FACT "

// timestampLayout is the time format of every log line.
const timestampLayout = "2006-01-02 15:04:05"

// Fact writes f to the log file only.
func (l *Logger) Fact(f Fact) {
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	l.writeLogOnly(factSeverity, "%s", data)
}

// RunFacts are the facts of one log file.
type RunFacts struct {
	// Backup is the result of the backup run, nil when the log has none.
	Backup *Fact
	// Sets holds what the backup run wrote per backup set name.
	Sets map[string]Fact
	// Verify holds the newest verification result per backup set name.
	Verify map[string]Fact
}

// ReadFacts reads the facts of the log file at path. Lines that are not
// facts, or facts it cannot parse, are skipped: a log written by an older
// version, or edited by hand, yields fewer facts, never an error. Only a log
// that cannot be read is an error.
func ReadFacts(path string) (RunFacts, error) {
	facts := RunFacts{Sets: make(map[string]Fact), Verify: make(map[string]Fact)}
	f, err := os.Open(path)
	if err != nil {
		return facts, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fact, ok := parseFact(scanner.Text())
		if !ok {
			continue
		}
		switch fact.Kind {
		case FactBackup:
			facts.Backup = &fact
		case FactSet:
			if fact.Set != "" {
				facts.Sets[fact.Set] = fact
			}
		case FactVerify:
			if fact.Set != "" {
				facts.Verify[fact.Set] = fact
			}
		}
	}
	return facts, scanner.Err()
}

// parseFact parses a line "[2006-01-02 15:04:05] FACT  - {...}".
func parseFact(line string) (Fact, bool) {
	var fact Fact
	if len(line) < len(timestampLayout)+2 || line[0] != '[' || line[len(timestampLayout)+1] != ']' {
		return fact, false
	}
	rest, ok := strings.CutPrefix(line[len(timestampLayout)+2:], " "+factSeverity+" - ")
	if !ok || json.Unmarshal([]byte(rest), &fact) != nil || fact.Kind == "" {
		return fact, false
	}
	fact.Time, _ = time.ParseInLocation(timestampLayout, line[1:len(timestampLayout)+1], time.Local)
	return fact, true
}
