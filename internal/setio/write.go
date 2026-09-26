// Package setio writes complete backup sets: it connects the TAR producer
// (package archive), the container writer (package container), and the split
// writer, and makes a set appear under its final file names only once it is
// complete.
package setio

import (
	"RestoreSafe/internal/archive"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/manifest"
	"RestoreSafe/internal/util"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

// Counters receive byte counts for progress and stall reporting. Any field may
// be nil.
type Counters struct {
	In    *atomic.Int64
	Out   *atomic.Int64
	Calls *atomic.Int64
}

// Base describes the full backup a differential is based on.
type Base struct {
	Header         *container.Header
	Manifest       *manifest.Manifest
	ManifestSHA256 string
}

// SetParams describes one backup set to write.
type SetParams struct {
	SourceDir   string
	ExcludeDirs []string
	// Exclude holds the configured exclude patterns; nil excludes nothing.
	Exclude *util.ExcludeMatcher
	// SkipUnreadable skips unreadable files instead of aborting.
	SkipUnreadable bool
	// OnSkip is called for every skipped file or directory; stale marks a
	// differential entry that keeps the full backup's older version.
	OnSkip func(rel, reason string, stale bool)
	// OutputDir receives the part files (the backup directory or a staging
	// directory).
	OutputDir string
	// Entry names the set. For a differential, Entry.ChainID is the base's
	// chain ID and Entry.DiffNumber the new differential number.
	Entry util.BackupEntry
	// Base makes the set a differential of that full backup; nil writes a
	// full backup.
	Base *Base
	RunID     util.BackupID
	KeySet    container.KeySet
	Master    []byte
	// SplitSizeBytes is the maximum part size.
	SplitSizeBytes int64
	// SyncParts flushes each part to disk before it is closed.
	SyncParts    bool
	OnPartOpened func(seq int, path string)
	Counters     Counters
}

// Result describes a written set.
type Result struct {
	// Parts are the final part paths in order.
	Parts    []string
	Write    *container.WriteResult
	Manifest manifest.Footer
	Stats    archive.BuildStats
}

// WriteSet writes a full backup of p.SourceDir, or a differential when p.Base
// is set. Parts are written with the temporary suffix and renamed to their
// final names only after the trailer is on disk, so an interrupted backup
// never looks like a complete set. On error, all parts written so far are
// removed.
func WriteSet(p SetParams) (*Result, error) {
	setType := manifest.SetTypeFull
	var baseManifest *manifest.Manifest
	if p.Base != nil {
		setType = manifest.SetTypeDiff
		baseManifest = p.Base.Manifest
		if p.Entry.DiffNumber < 1 || p.Base.Header.ChainID != string(p.Entry.ChainID) || p.Base.Header.DirectoryName != p.Entry.DirectoryName {
			return nil, fmt.Errorf("Internal error: differential %s does not match its base %s_%s.", p.Entry.String(), p.Base.Header.DirectoryName, p.Base.Header.ChainID)
		}
	} else if p.Entry.DiffNumber != 0 {
		return nil, fmt.Errorf("Internal error: differential %s has no base.", p.Entry.String())
	}

	h, err := container.NewHeader(setType, string(p.Entry.ChainID), string(p.RunID), p.Entry.DirectoryName, p.Entry.Date, p.KeySet)
	if err != nil {
		return nil, err
	}
	if p.Base != nil {
		h.DiffNumber = p.Entry.DiffNumber
		h.BaseDate = p.Base.Header.Date
		h.BaseManifestSHA256 = p.Base.ManifestSHA256
	}

	sw := util.NewWriter(func(seq int) string {
		return util.PartFileName(p.OutputDir, p.Entry, seq) + util.TempSuffix
	}, p.SplitSizeBytes)
	sw.SetSyncOnClose(p.SyncParts)
	if p.OnPartOpened != nil {
		sw.SetPartOpenedHook(func(seq int, path string) {
			p.OnPartOpened(seq, strings.TrimSuffix(path, util.TempSuffix))
		})
	}
	bw := bufio.NewWriterSize(sw, util.SplitWriteBufferSize)

	mb := manifest.NewBuilder(manifest.Header{
		SetType:       setType,
		ChainID:       string(p.Entry.ChainID),
		DiffNumber:    p.Entry.DiffNumber,
		DirectoryName: p.Entry.DirectoryName,
		SourcePath:    toSlash(p.SourceDir),
		Exclude:       p.Exclude.Patterns(),
	})

	var stats archive.BuildStats
	pr, pw := io.Pipe()
	tarErrCh := make(chan error, 1)
	go func() {
		err := archive.BuildTar(pw, archive.BuildOptions{
			SourceDir:      p.SourceDir,
			ExcludeDirs:    p.ExcludeDirs,
			Exclude:        p.Exclude,
			SkipUnreadable: p.SkipUnreadable,
			OnSkip:         p.OnSkip,
			Stats:          &stats,
			Base:           baseManifest,
		}, mb)
		pw.CloseWithError(err) //nolint:errcheck
		tarErrCh <- err
	}()

	var dataIn io.Reader = pr
	if p.Counters.In != nil {
		dataIn = &util.CountingReader{R: pr, Total: p.Counters.In}
	}
	var out io.Writer = bw
	if p.Counters.Out != nil {
		out = &util.CountingWriter{W: bw, Total: p.Counters.Out, Calls: p.Counters.Calls}
	}

	tarDone := false
	var tarErr error
	res, writeErr := container.Write(out, h, p.Master, p.SplitSizeBytes, dataIn, func() ([]byte, error) {
		tarErr = <-tarErrCh
		tarDone = true
		if tarErr != nil {
			return nil, tarErr
		}
		return mb.Bytes()
	})
	pr.CloseWithError(errors.New("backup aborted")) //nolint:errcheck
	if !tarDone {
		tarErr = <-tarErrCh
	}

	flushErr := bw.Flush()
	closeErr := sw.Close()
	tempParts := sw.Paths()

	if err := firstError(tarErr, writeErr, flushErr, closeErr); err != nil {
		removeAll(tempParts)
		if tarErr != nil {
			return nil, fmt.Errorf("Creating TAR failed: %w", tarErr)
		}
		return nil, err
	}
	if len(tempParts) != res.Trailer.PartCount {
		removeAll(tempParts)
		return nil, fmt.Errorf("Internal error: wrote %d part(s), trailer expects %d.", len(tempParts), res.Trailer.PartCount)
	}

	finalParts, err := FinalizeParts(tempParts)
	if err != nil {
		return nil, err
	}
	return &Result{Parts: finalParts, Write: res, Manifest: mb.Footer(), Stats: stats}, nil
}

// FinalizeParts renames temporary parts to their final names in ascending
// order. On error, the parts not yet renamed keep their temporary names and
// the set is reported as incomplete by the catalog.
func FinalizeParts(tempParts []string) ([]string, error) {
	final := make([]string, len(tempParts))
	for i, tmp := range tempParts {
		dst := strings.TrimSuffix(tmp, util.TempSuffix)
		if dst == tmp {
			return nil, fmt.Errorf("Internal error: part %q has no temporary suffix.", tmp)
		}
		if _, err := os.Stat(dst); err == nil {
			return nil, fmt.Errorf("Backup file %q already exists. Remedy: Remove or rename the existing file and start the backup again.", dst)
		}
		if err := os.Rename(tmp, dst); err != nil {
			return nil, fmt.Errorf("Failed to finalize part file %q: %w. Remedy: Check write permissions in the backup directory.", tmp, err)
		}
		final[i] = dst
	}
	return final, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func removeAll(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
