// Package restorepoint decrypts a restore point (a full backup, or a
// differential plus its full backup) and hands its files to restore or
// verify; backup uses it to check the sets it has just written.
package restorepoint

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/phsc84/restoresafe/internal/format/archive"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/manifest"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/problem"
)

// Output receives what reading a restore point reports: its log lines and
// the decrypted bytes, for the progress. Both may be nil.
type Output struct {
	Log  *logging.Logger
	Done *atomic.Int64
}

// reading is one pass over the data sections of a restore point.
type reading struct {
	out Output
	// name is the set's directory name, for the progress log.
	name string
	// verifyOnly checks the files without writing them.
	verifyOnly bool
}

// Restore restores the restore point of set into destDir. For a
// differential, base is its full backup: the differential's manifest is the
// target state, files unchanged since the full backup come from base, and
// new or changed files from the differential. Every file is checked against
// its manifest hash. It returns the target manifest so the caller can report
// skipped files, and stops when ctx is cancelled.
func Restore(ctx context.Context, set, base *container.Set, master []byte, destDir string, out Output) (*manifest.Manifest, error) {
	return read(ctx, set, base, master, destDir, reading{out: out, name: set.Header.DirectoryName})
}

// Verify checks the restore point of set as Restore restores it, without
// writing anything.
func Verify(ctx context.Context, set, base *container.Set, master []byte, out Output) (*manifest.Manifest, error) {
	return read(ctx, set, base, master, "", reading{out: out, name: set.Header.DirectoryName, verifyOnly: true})
}

func read(ctx context.Context, set, base *container.Set, master []byte, destDir string, rd reading) (*manifest.Manifest, error) {
	log := rd.out.Log
	if set.Header.IsDiff() && base == nil {
		return nil, problem.Errorf("The full backup of chain %s is required to restore %s_%s.", set.Header.ChainID, set.Header.DirectoryName, set.Header.Date).WithRemedy(fmt.Sprintf("Put the FULL files of %s into the backup directory.", set.Header.ChainID))
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

	r := archive.NewRestorer(m, destDir, rd.verifyOnly)
	if err := r.CreateDirectories(); err != nil {
		return nil, err
	}

	if set.Header.IsDiff() {
		log.Info("  Reading unchanged files from the full backup (%s)", base.Header.Date)
		err = rd.decrypt(ctx, base, baseKeys, func(tarStream io.Reader) error {
			return r.ExtractSection(tarStream, archive.DecideFromBase(m))
		})
		if err != nil {
			return nil, err
		}
		log.Info("  Reading new and changed files from differential %03d (%s)", set.Header.DiffNumber, set.Header.Date)
	}
	err = rd.decrypt(ctx, set, keys, func(tarStream io.Reader) error {
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
		return problem.Errorf("The base of %s_%s is not a full backup.", diff.DirectoryName, diff.Date).WithRemedy("Use unmodified backup files.")
	case base.ChainID != diff.ChainID || base.DirectoryName != diff.DirectoryName || base.Date != diff.BaseDate:
		return problem.Errorf("The full backup %s_%s_%s does not belong to differential %03d (expected full backup of %s).", base.DirectoryName, base.ChainID, base.Date, diff.DiffNumber, diff.BaseDate).WithRemedy("Use the FULL files that belong to this chain.")
	case base.KeySet.ID != diff.KeySet.ID:
		return problem.Errorf("The full backup of chain %s uses different keys than differential %03d.", diff.ChainID, diff.DiffNumber).WithRemedy("Use unmodified backup files.")
	case baseManifestSHA256 != diff.BaseManifestSHA256:
		return problem.Errorf("The full backup of chain %s is not the one differential %03d was created from (manifest checksum mismatch).", diff.ChainID, diff.DiffNumber).WithRemedy("Use the original FULL files of this chain.")
	}
	return nil
}

// VerifyOwnData checks a set's own data section against its manifest: every
// file stored in the set is decrypted and hashed. For a differential this
// covers the new and changed files without reading the full backup, which is
// what verify_after_backup needs right after writing it. ctx and out work as
// in Restore.
func VerifyOwnData(ctx context.Context, set *container.Set, master []byte, out Output) (*manifest.Manifest, error) {
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
	err = reading{out: out, name: set.Header.DirectoryName, verifyOnly: true}.decrypt(ctx, set, keys, func(tarStream io.Reader) error {
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
