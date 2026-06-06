// Package naming provides helpers for generating and parsing backup file names.
//
// Naming scheme:
//
//	[SourceDirectoryName]_YYYY-MM-DD_ABC123-{Seq}.enc
//	[SourceDirectoryName]_YYYY-MM-DD_ABC123.challenge  (YubiKey challenge file)
//
// The backup ID (ABC123) is a random 6-character string drawn from [A-Z0-9].
package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"
	"regexp"
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

// BackupID is a random 6-character identifier for a single backup run.
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

// PartFileName returns the path for a backup part file.
//
//	{dir}/[directoryName]_YYYY-MM-DD_{id}-{seq:03d}.enc
func PartFileName(dir, directoryName, date string, id BackupID, seq int) string {
	name := fmt.Sprintf("[%s]_%s_%s-%03d.enc", directoryName, date, string(id), seq)
	return filepath.Join(dir, name)
}

// LogFileName returns the path for the log file of a backup run.
//
//	{dir}/YYYY-MM-DD_{id}.log
func LogFileName(dir, date string, id BackupID) string {
	name := fmt.Sprintf("%s_%s.log", date, string(id))
	return filepath.Join(dir, name)
}

// ChallengeFileName returns the path for the YubiKey challenge file.
//
//	{dir}/[directoryName]_YYYY-MM-DD_{id}.challenge
func ChallengeFileName(dir, directoryName, date string, id BackupID) string {
	name := fmt.Sprintf("[%s]_%s_%s.challenge", directoryName, date, string(id))
	return filepath.Join(dir, name)
}

// BackupEntry represents one logical backup (all parts of one source directory).
type BackupEntry struct {
	DirectoryName string
	Date       string
	ID         BackupID
}

// String returns the display name without part/extension.
func (e BackupEntry) String() string {
	return fmt.Sprintf("%s_%s_%s", e.DirectoryName, e.Date, string(e.ID))
}

// RunKey returns a unique key for the backup run (date + ID).
// Used for deduplication and map lookups across catalog, retention, and health checks.
func (e BackupEntry) RunKey() string {
	return e.Date + "|" + string(e.ID)
}

// partFilePattern matches:  [name]_{YYYY-MM-DD}_{ID}-{seq}.enc
// Named capture groups:
//
//	1 (.+?)              - Directory name (non-greedy)
//	2 (\d{4}-\d{2}-\d{2}) - Date in YYYY-MM-DD format
//	3 ([A-Z0-9]{6})      - 6-character backup ID
//	4 (\d{3})            - 3-digit sequence number (001, 002, ...)
var partFilePattern = regexp.MustCompile(
	`^\[(.+?)\]_(\d{4}-\d{2}-\d{2})_([A-Z0-9]{6})-(\d{3})\.enc$`,
)

// ParsePartFileName tries to parse a .enc filename.
// Returns (entry, seq, true) on success.
func ParsePartFileName(basename string) (BackupEntry, int, bool) {
	m := partFilePattern.FindStringSubmatch(basename)
	if m == nil {
		return BackupEntry{}, 0, false
	}
	var seq int
	fmt.Sscanf(m[4], "%d", &seq)
	return BackupEntry{
		DirectoryName: m[1],
		Date:       m[2],
		ID:         BackupID(m[3]),
	}, seq, true
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
	remedy := "Remedy: This backup's filename is malformed or unsafe; rename the .enc file(s) to a valid [name]_DATE_ID-SEQ.enc pattern."

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
