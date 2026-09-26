package catalog

import (
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ScanBackups lists every backup set (complete part-file names only) in
// backupDir.
func ScanBackups(backupDir string) ([]util.BackupEntry, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}

	seen := make(map[util.BackupEntry]bool)
	var result []util.BackupEntry
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		entry, _, ok := util.ParsePartFileName(de.Name())
		if !ok || seen[entry] {
			continue
		}
		seen[entry] = true
		result = append(result, entry)
	}
	return result, nil
}

// CollectParts returns the part file paths of an entry, sorted by part number.
func CollectParts(backupDir string, entry util.BackupEntry) ([]string, error) {
	des, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("Failed to read backup directory %q: %w", backupDir, err)
	}

	type seqPath struct {
		seq  int
		path string
	}
	var parts []seqPath
	for _, de := range des {
		e, seq, ok := util.ParsePartFileName(de.Name())
		if !ok || e != entry {
			continue
		}
		parts = append(parts, seqPath{seq, filepath.Join(backupDir, de.Name())})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].seq < parts[j].seq })

	paths := make([]string, len(parts))
	for i, p := range parts {
		paths[i] = p.path
	}
	return paths, nil
}

// SetInfo is the password-free inspection result of one backup set.
type SetInfo struct {
	Entry     util.BackupEntry
	Parts     []string
	SizeBytes int64
	Header    *container.Header
	Trailer   container.Trailer
	// Err is set when the set is incomplete or invalid; Header is nil then.
	Err error
}

// Complete reports whether the set passed every structural check.
func (s SetInfo) Complete() bool { return s.Err == nil && s.Header != nil }

// Created returns the creation time from the header, or the zero time.
func (s SetInfo) Created() time.Time {
	if s.Header == nil {
		return time.Time{}
	}
	return s.Header.Created()
}

// OpenSet opens a backup set after checking part continuity and that the
// file names match the set header. The caller must Close the set.
func OpenSet(backupDir string, entry util.BackupEntry) (*container.Set, error) {
	parts, err := CollectParts(backupDir, entry)
	if err != nil {
		return nil, err
	}
	if err := checkContinuity(parts); err != nil {
		return nil, err
	}
	set, err := container.Open(parts)
	if err != nil {
		return nil, err
	}
	if err := checkNameMatchesHeader(entry, set.Header); err != nil {
		set.Close() //nolint:errcheck
		return nil, err
	}
	return set, nil
}

// InspectSet opens and closes a set to report its status.
func InspectSet(backupDir string, entry util.BackupEntry) SetInfo {
	info := SetInfo{Entry: entry}
	parts, err := CollectParts(backupDir, entry)
	if err != nil {
		info.Err = err
		return info
	}
	info.Parts = parts
	for _, p := range parts {
		if fi, err := os.Stat(p); err == nil {
			info.SizeBytes += fi.Size()
		}
	}
	set, err := OpenSet(backupDir, entry)
	if err != nil {
		info.Err = err
		return info
	}
	defer set.Close()
	info.Header = set.Header
	info.Trailer = set.Trailer
	return info
}

// Inventory inspects every backup set in backupDir, newest first.
func Inventory(backupDir string) ([]SetInfo, error) {
	entries, err := ScanBackups(backupDir)
	if err != nil {
		return nil, err
	}
	infos := make([]SetInfo, 0, len(entries))
	for _, e := range entries {
		infos = append(infos, InspectSet(backupDir, e))
	}
	sortNewestFirst(infos)
	return infos, nil
}

func sortNewestFirst(infos []SetInfo) {
	sort.SliceStable(infos, func(i, j int) bool {
		ti, tj := infos[i].Created(), infos[j].Created()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		if infos[i].Entry.Date != infos[j].Entry.Date {
			return infos[i].Entry.Date > infos[j].Entry.Date
		}
		return infos[i].Entry.String() < infos[j].Entry.String()
	})
}

// SelectInfos returns the inventory entries of selected, in selection order.
// Entries not in infos are skipped.
func SelectInfos(infos []SetInfo, selected []util.BackupEntry) []SetInfo {
	byEntry := make(map[util.BackupEntry]SetInfo, len(infos))
	for _, info := range infos {
		byEntry[info.Entry] = info
	}
	out := make([]SetInfo, 0, len(selected))
	for _, e := range selected {
		if info, ok := byEntry[e]; ok {
			out = append(out, info)
		}
	}
	return out
}

// BaseOf returns the full backup of a differential's chain, or an error that
// explains why it cannot be used. infos is the inventory.
func BaseOf(infos []SetInfo, diff util.BackupEntry) (*SetInfo, error) {
	for i := range infos {
		info := &infos[i]
		if info.Entry.IsDiff() || info.Entry.DirectoryName != diff.DirectoryName || info.Entry.ChainID != diff.ChainID {
			continue
		}
		if !info.Complete() {
			return nil, fmt.Errorf("The full backup %s of %s is incomplete: %v", info.Entry.String(), diff.String(), info.Err)
		}
		return info, nil
	}
	return nil, fmt.Errorf("%s cannot be restored: the full backup [%s]_%s_*_FULL-*.enc of chain %s is missing. Remedy: Put the FULL files of %s into the backup directory.", diff.String(), diff.DirectoryName, diff.ChainID, diff.ChainID, diff.ChainID)
}

// KeySetMismatch returns why ks no longer matches the configuration (so the
// next backup must create new keys), or "" when it matches.
func KeySetMismatch(cfg *util.Config, ks *container.KeySet) string {
	switch {
	case ks.AuthMode != int(cfg.AuthenticationMode):
		return "authentication_mode changed in config.yaml"
	case cfg.YubiKeySpare && ks.YubiKeyCount() < 2:
		return "yubikey_spare enabled in config.yaml"
	case !cfg.YubiKeySpare && ks.YubiKeyCount() > 1:
		return "yubikey_spare disabled in config.yaml"
	case cfg.RecoveryCode && !ks.HasSlotType(container.SlotRecovery):
		return "recovery_code enabled in config.yaml"
	case !cfg.RecoveryCode && ks.HasSlotType(container.SlotRecovery):
		return "recovery_code disabled in config.yaml"
	}
	return ""
}

// CurrentKeySet returns the key set of the newest complete set, or nil when
// the backup directory holds no complete RestoreSafe 2 backup. infos must be
// sorted newest first (as returned by Inventory).
func CurrentKeySet(infos []SetInfo) *container.KeySet {
	for _, info := range infos {
		if info.Complete() {
			ks := info.Header.KeySet
			return &ks
		}
	}
	return nil
}

func checkContinuity(parts []string) error {
	if len(parts) == 0 {
		return fmt.Errorf("No part files found. Remedy: Ensure the .enc files are present in the backup directory.")
	}
	for i, p := range parts {
		_, seq, _ := util.ParsePartFileName(filepath.Base(p))
		if seq != i+1 {
			return fmt.Errorf("Missing part file %03d. Remedy: Restore the missing .enc part or create a new backup.", i+1)
		}
	}
	return nil
}

func checkNameMatchesHeader(entry util.BackupEntry, h *container.Header) error {
	if h.DirectoryName != entry.DirectoryName || h.ChainID != string(entry.ChainID) || h.Date != entry.Date || h.DiffNumber != entry.DiffNumber || h.IsDiff() != entry.IsDiff() {
		return fmt.Errorf("File name does not match the backup header (header: %s_%s_%s, type %s). Remedy: Do not rename backup files; restore the original file names.", h.DirectoryName, h.ChainID, h.Date, h.SetType)
	}
	return nil
}

// IsIncomplete reports whether err marks an incomplete set (interrupted
// backup or missing parts), as opposed to an unreadable or invalid one.
func IsIncomplete(err error) bool {
	var inc *container.ErrIncomplete
	return errors.As(err, &inc)
}

// ListTempParts returns the names of RestoreSafe part files still carrying
// the temporary suffix (left behind by an interrupted backup).
func ListTempParts(backupDir string) ([]string, error) {
	des, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		if _, _, ok := util.ParseTempPartFileName(de.Name()); ok {
			out = append(out, de.Name())
		}
	}
	return out, nil
}

// ListLegacyFiles returns the names of RestoreSafe 1.x backup files.
func ListLegacyFiles(backupDir string) ([]string, error) {
	des, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		if !de.IsDir() && util.IsLegacyBackupFileName(de.Name()) {
			out = append(out, de.Name())
		}
	}
	return out, nil
}
