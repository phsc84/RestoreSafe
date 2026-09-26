package archive

import (
	"RestoreSafe/internal/manifest"
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// BuildOptions configures BuildTar.
type BuildOptions struct {
	// SourceDir is the directory to back up.
	SourceDir string
	// ExcludeDirs are absolute directories to skip (e.g. the backup directory
	// when it lies inside the source).
	ExcludeDirs []string
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// BuildTar walks opts.SourceDir, writes the content of every regular file as a
// TAR stream to w, and adds one manifest entry per directory and file to mb.
// The TAR contains only regular files; directories and all metadata live in
// the manifest. Symlinks, junctions, and other special entries are skipped.
func BuildTar(w io.Writer, opts BuildOptions, mb *manifest.Builder) error {
	cw := &countingWriter{w: w}
	tw := tar.NewWriter(cw)

	srcDir := filepath.Clean(opts.SourceDir)
	excludes := normalizeExcludes(srcDir, opts.ExcludeDirs)

	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("Failed to scan source directory at %q: %w", path, err)
		}
		if path == srcDir {
			return nil
		}
		if isExcluded(path, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("Failed to inspect %q: %w", path, err)
		}
		if !isRegularOrDir(info) {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("Failed to compute relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if err := manifest.ValidatePath(rel); err != nil {
			return err
		}

		if info.IsDir() {
			meta, err := statBasic(path)
			if err != nil {
				return fmt.Errorf("Failed to read metadata of directory %q: %w", path, err)
			}
			mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeDir, ModTime: meta.ModTime, CreationTime: meta.CreationTime, Attributes: meta.Attributes})
			return nil
		}

		entry, err := writeFile(tw, cw, path, rel)
		if err != nil {
			return err
		}
		mb.Add(entry)
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("Failed to finish TAR stream: %w", err)
	}
	return nil
}

// writeFile copies one file into the TAR stream and returns its manifest
// entry. The file must keep its size while it is copied; a file that shrinks
// or grows aborts the backup rather than storing a torn copy.
func writeFile(tw *tar.Writer, cw *countingWriter, path, rel string) (manifest.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to open file %q: %w. Remedy: Close programs that lock the file and check read permissions.", path, err)
	}
	defer f.Close()

	meta, err := basicInfoFromHandle(windows.Handle(f.Fd()))
	if err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to read metadata of %q: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to inspect %q: %w", path, err)
	}
	size := fi.Size()

	// Flush the padding of the previous entry so the counter points exactly
	// at this entry's header.
	if err := tw.Flush(); err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to write TAR stream: %w", err)
	}
	offset := cw.n
	hdr := &tar.Header{Name: rel, Size: size, Mode: 0o600, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := tw.WriteHeader(hdr); err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to write TAR header for %q: %w", path, err)
	}

	hasher := sha256.New()
	copied, err := io.CopyN(io.MultiWriter(tw, hasher), f, size)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return manifest.Entry{}, fmt.Errorf("File %q became smaller during backup (%d of %d bytes). Remedy: Close programs that modify the file and start the backup again.", path, copied, size)
		}
		return manifest.Entry{}, fmt.Errorf("Failed to read file %q: %w", path, err)
	}
	var probe [1]byte
	if n, _ := f.Read(probe[:]); n > 0 {
		return manifest.Entry{}, fmt.Errorf("File %q grew during backup. Remedy: Close programs that modify the file and start the backup again.", path)
	}

	return manifest.Entry{
		Path:         rel,
		Type:         manifest.TypeFile,
		Size:         size,
		ModTime:      meta.ModTime,
		ChangeTime:   meta.ChangeTime,
		CreationTime: meta.CreationTime,
		Attributes:   meta.Attributes,
		Hash:         hex.EncodeToString(hasher.Sum(nil)),
		Origin:       manifest.OriginFull,
		Offset:       &offset,
	}, nil
}

func normalizeExcludes(srcDir string, excludeDirs []string) []string {
	out := make([]string, 0, len(excludeDirs))
	for _, e := range excludeDirs {
		if e == "" {
			continue
		}
		ce := filepath.Clean(e)
		rel, err := filepath.Rel(srcDir, ce)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, ce)
	}
	return out
}

func isExcluded(path string, excludes []string) bool {
	for _, ex := range excludes {
		if strings.EqualFold(path, ex) {
			return true
		}
		if rel, err := filepath.Rel(ex, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "." {
			return true
		}
	}
	return false
}
