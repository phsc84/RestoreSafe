// Package restorepoint decrypts a restore point (a full backup, or a
// differential plus its full backup) and hands its files to restore or
// verify; backup uses it to check the sets it has just written.
package restorepoint

import (
	"RestoreSafe/internal/format/archive"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/manifest"
	"RestoreSafe/internal/logging"
	"context"
	"fmt"
	"io"
	"sync/atomic"
)

// Process restores the restore point of set into destDir, or, with
// verifyOnly, checks it without writing anything. For a differential, base is
// its full backup: the differential's manifest is the target state, files
// unchanged since the full backup come from base, and new or changed files
// from the differential. Every file is checked against its manifest hash. It
// returns the target manifest so the caller can report skipped files. It
// stops when ctx is cancelled and adds the decrypted bytes to done (may be
// nil).
func Process(ctx context.Context, set, base *container.Set, master []byte, destDir string, verifyOnly bool, log *logging.Logger, done *atomic.Int64) (*manifest.Manifest, error) {
	if set.Header.IsDiff() && base == nil {
		return nil, fmt.Errorf("The full backup of chain %s is required to restore %s_%s. Remedy: Put the FULL files of %s into the backup directory.", set.Header.ChainID, set.Header.DirectoryName, set.Header.Date, set.Header.ChainID)
	}
	keys, err := set.SectionKeys(master)
	if err != nil {
		return nil, err
	}
	defer keys.Zero()

	m, _, err := set.ReadManifest(keys)
	if err != nil {
		return nil, err
	}

	var baseKeys *container.SectionKeys
	if set.Header.IsDiff() {
		baseKeys, err = base.SectionKeys(master)
		if err != nil {
			return nil, err
		}
		defer baseKeys.Zero()
		_, baseSum, err := base.ReadManifest(baseKeys)
		if err != nil {
			return nil, fmt.Errorf("Full backup of chain %s: %w", base.Header.ChainID, err)
		}
		if err := checkBaseLink(set.Header, base.Header, baseSum); err != nil {
			return nil, err
		}
	}

	r := archive.NewRestorer(m, destDir, verifyOnly)
	if err := r.CreateDirectories(); err != nil {
		return nil, err
	}

	verb, prefix := "decrypted", "Extraction"
	if verifyOnly {
		verb, prefix = "verified", "Verification"
	}
	if set.Header.IsDiff() {
		log.Info("  Reading unchanged files from the full backup (%s)", base.Header.Date)
		err = RunSectionPipeline(ctx, base, baseKeys, log, set.Header.DirectoryName, verb, prefix, done, func(tarStream io.Reader) error {
			return r.ExtractSection(tarStream, archive.DecideFromBase(m))
		})
		if err != nil {
			return nil, err
		}
		log.Info("  Reading new and changed files from differential %03d (%s)", set.Header.DiffNumber, set.Header.Date)
	}
	err = RunSectionPipeline(ctx, set, keys, log, set.Header.DirectoryName, verb, prefix, done, func(tarStream io.Reader) error {
		return r.ExtractSection(tarStream, archive.DecideOwn(m))
	})
	if err != nil {
		return nil, err
	}
	if err := r.Finish(); err != nil {
		for _, p := range r.MissingFiles() {
			log.WarnLogOnly("  Missing in backup data: %s", p)
		}
		return nil, err
	}
	return m, nil
}

// checkBaseLink confirms that base is exactly the full backup the
// differential was written against.
func checkBaseLink(diff, base *container.Header, baseManifestSHA256 string) error {
	switch {
	case base.IsDiff():
		return fmt.Errorf("The base of %s_%s is not a full backup. Remedy: Use unmodified backup files.", diff.DirectoryName, diff.Date)
	case base.ChainID != diff.ChainID || base.DirectoryName != diff.DirectoryName || base.Date != diff.BaseDate:
		return fmt.Errorf("The full backup %s_%s_%s does not belong to differential %03d (expected full backup of %s). Remedy: Use the FULL files that belong to this chain.", base.DirectoryName, base.ChainID, base.Date, diff.DiffNumber, diff.BaseDate)
	case base.KeySet.ID != diff.KeySet.ID:
		return fmt.Errorf("The full backup of chain %s uses different keys than differential %03d. Remedy: Use unmodified backup files.", diff.ChainID, diff.DiffNumber)
	case baseManifestSHA256 != diff.BaseManifestSHA256:
		return fmt.Errorf("The full backup of chain %s is not the one differential %03d was created from (manifest checksum mismatch). Remedy: Use the original FULL files of this chain.", diff.ChainID, diff.DiffNumber)
	}
	return nil
}

// VerifyOwnData checks a set's own data section against its manifest: every
// file stored in the set is decrypted and hashed. For a differential this
// covers the new and changed files without reading the full backup, which is
// what verify_after_backup needs right after writing it. ctx and done work as
// in Process.
func VerifyOwnData(ctx context.Context, set *container.Set, master []byte, log *logging.Logger, done *atomic.Int64) (*manifest.Manifest, error) {
	keys, err := set.SectionKeys(master)
	if err != nil {
		return nil, err
	}
	defer keys.Zero()
	m, _, err := set.ReadManifest(keys)
	if err != nil {
		return nil, err
	}
	r := archive.NewRestorer(m, "", true)
	r.ExpectOwnContentOnly()
	err = RunSectionPipeline(ctx, set, keys, log, set.Header.DirectoryName, "verified", "Verification", done, func(tarStream io.Reader) error {
		return r.ExtractSection(tarStream, archive.DecideOwn(m))
	})
	if err != nil {
		return nil, err
	}
	if err := r.Finish(); err != nil {
		return nil, err
	}
	return m, nil
}

// ReportSkippedFiles logs the files that could not be read during the backup
// and are therefore missing from the restore point. It returns their count.
func ReportSkippedFiles(m *manifest.Manifest, directoryName string, log *logging.Logger) int {
	skipped := archive.SkippedFiles(m)
	if len(skipped) == 0 {
		return 0
	}
	log.Warn("  [%s] %d file(s) are not in this backup because they could not be read during backup:", directoryName, len(skipped))
	for _, p := range skipped {
		log.Warn("    %s", p)
	}
	return len(skipped)
}

// ReportStaleFiles logs the files a differential could not read, which the
// restore point holds in their older version from the full backup. It
// returns their count.
func ReportStaleFiles(m *manifest.Manifest, directoryName string, log *logging.Logger) int {
	stale := archive.StaleFiles(m)
	if len(stale) == 0 {
		return 0
	}
	log.Warn("  [%s] %d file(s) are restored in the older version of the full backup, because they could not be read when this differential was created:", directoryName, len(stale))
	for _, p := range stale {
		log.Warn("    %s", p)
	}
	return len(stale)
}

// SectionSize is the Progress total of processing set: the length of its
// data section and, for a differential, of its full backup's (base may be
// nil).
func SectionSize(set, base *container.Set) int64 {
	total := set.Trailer.DataLength
	if base != nil {
		total += base.Trailer.DataLength
	}
	return total
}
