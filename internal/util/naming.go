// Package naming provides helpers for generating and parsing backup file names.
//
// Naming scheme:
//
//	[SourceFolderName]_YYYY-MM-DD_ABC123-{Seq}.enc
//	[SourceFolderName]_YYYY-MM-DD_ABC123.challenge  (YubiKey challenge file)
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
//	{targetDir}/[folderName]_YYYY-MM-DD_{id}-{seq:03d}.enc
func PartFileName(targetDir, folderName, date string, id BackupID, seq int) string {
	name := fmt.Sprintf("[%s]_%s_%s-%03d.enc", folderName, date, string(id), seq)
	return filepath.Join(targetDir, name)
}

// LogFileName returns the path for the log file of a backup run.
//
//	{targetDir}/YYYY-MM-DD_{id}.log
func LogFileName(targetDir, date string, id BackupID) string {
	name := fmt.Sprintf("%s_%s.log", date, string(id))
	return filepath.Join(targetDir, name)
}

// ChallengeFileName returns the path for the YubiKey challenge file.
//
//	{targetDir}/[folderName]_YYYY-MM-DD_{id}.challenge
func ChallengeFileName(targetDir, folderName, date string, id BackupID) string {
	name := fmt.Sprintf("[%s]_%s_%s.challenge", folderName, date, string(id))
	return filepath.Join(targetDir, name)
}

// BackupEntry represents one logical backup (all parts of one source folder).
type BackupEntry struct {
	FolderName string
	Date       string
	ID         BackupID
}

// String returns the display name without part/extension.
func (e BackupEntry) String() string {
	return fmt.Sprintf("%s_%s_%s", e.FolderName, e.Date, string(e.ID))
}

// partFilePattern matches:  [name]_{YYYY-MM-DD}_{ID}-{seq}.enc
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
		FolderName: m[1],
		Date:       m[2],
		ID:         BackupID(m[3]),
	}, seq, true
}

// FolderBaseName returns the last element of a path.
func FolderBaseName(path string) string {
	base := filepath.Base(strings.TrimRight(filepath.Clean(path), string(filepath.Separator)))
	return base
}
