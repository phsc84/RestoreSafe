// Package naming provides helpers for generating and parsing backup file names.
//
// Naming scheme (RestoreSafe 2):
//
//	[SourceDirectoryName]_CHAINID_YYYY-MM-DD_FULL-{Part}.enc      full backup
//	[SourceDirectoryName]_CHAINID_YYYY-MM-DD_DIFF{NNN}-{Part}.enc differential NNN of the chain
//	YYYY-MM-DD_RUNID.log                                          log of one backup run
//
// The chain ID is the ID of the chain's full backup, so all files of a chain
// share it and sort together. IDs are random 6-character strings drawn from
// [A-Z0-9]. Parts are written as "<name>.tmp" and renamed once complete.
package naming

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	idAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	idLength   = 6
)

// MaxPartSequence is the highest part sequence number representable by the
// naming scheme. Part files are named with a fixed 3-digit sequence (%03d) and
// the discovery regex matches exactly three digits, so a backup that produces
// more than 999 parts writes files (e.g. ...-1000.enc) that can never be
// discovered, inspected, or restored. Backups that would exceed this limit are
// rejected during preflight to prevent silent, unrecoverable data loss.
const MaxPartSequence = 999

// MaxDiffNumber is the highest differential number within a chain.
const MaxDiffNumber = 999

// TempSuffix is appended to part files while a backup set is being written.
const TempSuffix = ".tmp"

// BackupID is a random 6-character identifier of a backup run or chain.
type BackupID string

// NewBackupID generates a cryptographically random 6-character backup ID.
func NewBackupID() (BackupID, error) {
	result := make([]byte, idLength)
	alphabetLen := big.NewInt(int64(len(idAlphabet)))

	for i := range result {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("Failed to generate backup ID: %w", err)
		}
		result[i] = idAlphabet[n.Int64()]
	}

	return BackupID(result), nil
}

// DateString returns today's date in YYYY-MM-DD format.
func DateString() string {
	return time.Now().Format("2006-01-02")
}

// BackupEntry identifies one backup set: all parts of one source directory
// written in one run.
type BackupEntry struct {
	DirectoryName string
	ChainID       BackupID
	Date          string
	// DiffNumber is 0 for a full backup and 1-999 for a differential.
	DiffNumber int
}

// IsDiff reports whether the entry is a differential backup.
func (e BackupEntry) IsDiff() bool { return e.DiffNumber > 0 }

// TypeLabel returns "FULL" or "DIFFnnn" as used in file names.
func (e BackupEntry) TypeLabel() string {
	if e.IsDiff() {
		return fmt.Sprintf("DIFF%03d", e.DiffNumber)
	}
	return "FULL"
}

// String returns the display name without part/extension, e.g.
// "Documents_ABC123_2026-09-01_FULL".
func (e BackupEntry) String() string {
	return fmt.Sprintf("%s_%s_%s_%s", e.DirectoryName, string(e.ChainID), e.Date, e.TypeLabel())
}

// ChainKey identifies the chain of the entry (directory + chain ID).
func (e BackupEntry) ChainKey() string {
	return e.DirectoryName + "|" + string(e.ChainID)
}

// PartFileName returns the path for a backup part file.
func PartFileName(dir string, e BackupEntry, seq int) string {
	name := fmt.Sprintf("[%s]_%s_%s_%s-%03d.enc", e.DirectoryName, string(e.ChainID), e.Date, e.TypeLabel(), seq)
	return filepath.Join(dir, name)
}

// LogFileName returns the path for the log file of a backup run.
//
//	{dir}/YYYY-MM-DD_{runID}.log
func LogFileName(dir, date string, runID BackupID) string {
	name := fmt.Sprintf("%s_%s.log", date, string(runID))
	return filepath.Join(dir, name)
}

// partFilePattern matches [name]_{ID}_{YYYY-MM-DD}_{FULL|DIFFnnn}-{seq}.enc
// with an optional ".tmp" suffix for parts still being written.
var partFilePattern = regexp.MustCompile(
	`^\[(.+?)\]_([A-Z0-9]{6})_(\d{4}-\d{2}-\d{2})_(FULL|DIFF(\d{3}))-(\d{3})\.enc(\.tmp)?$`,
)

// ParsePartFileName parses a complete (non-temporary) part file name.
// Returns (entry, seq, true) on success.
func ParsePartFileName(basename string) (BackupEntry, int, bool) {
	entry, seq, temp, ok := parsePartFileName(basename)
	if !ok || temp {
		return BackupEntry{}, 0, false
	}
	return entry, seq, true
}

// ParseTempPartFileName parses the name of a part that is still being written
// (or was left behind by an interrupted backup).
func ParseTempPartFileName(basename string) (BackupEntry, int, bool) {
	entry, seq, temp, ok := parsePartFileName(basename)
	if !ok || !temp {
		return BackupEntry{}, 0, false
	}
	return entry, seq, true
}

func parsePartFileName(basename string) (BackupEntry, int, bool, bool) {
	m := partFilePattern.FindStringSubmatch(basename)
	if m == nil {
		return BackupEntry{}, 0, false, false
	}
	seq, _ := strconv.Atoi(m[6])
	if seq < 1 {
		return BackupEntry{}, 0, false, false
	}
	diff := 0
	if m[5] != "" {
		diff, _ = strconv.Atoi(m[5])
		if diff < 1 {
			return BackupEntry{}, 0, false, false
		}
	}
	return BackupEntry{
		DirectoryName: m[1],
		ChainID:       BackupID(m[2]),
		Date:          m[3],
		DiffNumber:    diff,
	}, seq, m[7] != "", true
}

// legacyFilePattern matches RestoreSafe 1.x part and challenge files.
var legacyFilePattern = regexp.MustCompile(
	`^\[(.+?)\]_(\d{4}-\d{2}-\d{2})_([A-Z0-9]{6})(-\d{3}\.enc|\.challenge)$`,
)

// IsLegacyBackupFileName reports whether basename is a RestoreSafe 1.x backup
// file (part or challenge file). RestoreSafe 2 never modifies these files.
func IsLegacyBackupFileName(basename string) bool {
	return legacyFilePattern.MatchString(basename)
}

// LegacyLogFileName returns the base name of the log file belonging to a
// RestoreSafe 1.x backup file, so retention can leave 1.x logs untouched.
func LegacyLogFileName(basename string) (string, bool) {
	m := legacyFilePattern.FindStringSubmatch(basename)
	if m == nil {
		return "", false
	}
	return m[2] + "_" + m[3] + ".log", true
}

// reservedWindowsNames are device names Windows refuses to use as a path
// component, regardless of extension (e.g. "CON.txt" is also reserved).
var reservedWindowsNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// ValidateBackupEntryName checks that a parsed backup directory name is safe to
// use as a single path component when constructing restore output directories.
//
// Defense-in-depth: a malicious .enc filename in the backup directory could
// carry a directory name like "." or ".." that, once joined to the restore
// destination, resolves outside the intended target. Real cases are already
// blocked indirectly (filename components cannot contain separators, and the
// os.Mkdir/os.Stat guards in the restore flow reject "."/".."), but validating
// explicitly turns those failures into a clear, early preflight error and makes
// the invariant survive future refactors.
func ValidateBackupEntryName(name string) error {
	remedy := "Remedy: This backup's filename is malformed or unsafe; rename the .enc file(s) to a valid [name]_ID_DATE_FULL-SEQ.enc pattern."

	if name == "" {
		return fmt.Errorf("Backup directory name is empty. %s", remedy)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("Backup directory name %q is a relative path element. %s", name, remedy)
	}
	if strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("Backup directory name %q contains a path separator or drive marker. %s", name, remedy)
	}
	// Windows strips trailing dots and spaces, which can cause two distinct
	// names to collide on the filesystem.
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("Backup directory name %q ends with a dot or space. %s", name, remedy)
	}
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if _, reserved := reservedWindowsNames[strings.ToUpper(base)]; reserved {
		return fmt.Errorf("Backup directory name %q is a reserved Windows device name. %s", name, remedy)
	}
	return nil
}

// DirectoryBaseName returns the last element of a path.
func DirectoryBaseName(path string) string {
	base := filepath.Base(strings.TrimRight(filepath.Clean(path), string(filepath.Separator)))
	return base
}

// logFilePattern matches the log file of a 2.0 backup run:
// {YYYY-MM-DD}_{runID}.log.
var logFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})_([A-Z0-9]{6})\.log$`)

// ParseLogFileName parses the name of a run's log file (see LogFileName).
func ParseLogFileName(basename string) (date string, runID BackupID, ok bool) {
	m := logFilePattern.FindStringSubmatch(basename)
	if m == nil {
		return "", "", false
	}
	return m[1], BackupID(m[2]), true
}
