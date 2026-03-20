// Package split provides a SplitWriter that transparently distributes a
// continuous byte stream across multiple fixed-size files.
package util

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SplitWriteBufferSize is the buffered writer size used before split output writes.
const SplitWriteBufferSize = 32 * 1024 * 1024

// NameFunc is called to produce the file path for each part.
// seq is the 1-based sequence number.
type NameFunc func(seq int) string

// Writer writes data to a series of sequentially named files.
// Each file is at most maxBytes bytes. When a file is full, it is closed and
// the next file is opened transparently.
type Writer struct {
	nameFunc NameFunc
	maxBytes int64
	seq      int
	written  int64
	current  *os.File
	paths    []string

	fileWriteCalls int64
	fileWriteBytes int64
	partsOpened    int
	partsClosed    int
}

// WriteStats contains low-level output stats of the split writer.
type WriteStats struct {
	FileWriteCalls int64
	FileWriteBytes int64
	PartsOpened    int
	PartsClosed    int
}

// NewWriter creates a SplitWriter. maxBytes is the maximum number of bytes per part.
func NewWriter(nameFunc NameFunc, maxBytes int64) *Writer {
	return &Writer{
		nameFunc: nameFunc,
		maxBytes: maxBytes,
	}
}

// Write implements io.Writer. It splits data across files as needed.
func (s *Writer) Write(p []byte) (int, error) {
	if s.maxBytes <= 0 {
		return 0, fmt.Errorf("Invalid split part size: %d. Remedy: Configure split_size_mb to a value greater than 0.", s.maxBytes)
	}

	total := 0
	for len(p) > 0 {
		if s.current == nil {
			if err := s.openNext(); err != nil {
				return total, err
			}
		}

		remaining := s.maxBytes - s.written
		n := int64(len(p))
		if n > remaining {
			n = remaining
		}

		written, err := s.current.Write(p[:n])
		total += written
		s.written += int64(written)
		s.fileWriteCalls++
		s.fileWriteBytes += int64(written)
		p = p[written:]

		if err != nil {
			return total, fmt.Errorf("Failed to write to part file: %w. Remedy: Check target-folder write permissions and free disk space.", err)
		}

		if s.written >= s.maxBytes {
			if err := s.closeCurrent(); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

// Close closes the current open part file (if any).
func (s *Writer) Close() error {
	return s.closeCurrent()
}

// Paths returns the paths of all created part files in order.
func (s *Writer) Paths() []string {
	return s.paths
}

// Stats returns collected low-level I/O statistics.
func (s *Writer) Stats() WriteStats {
	return WriteStats{
		FileWriteCalls: s.fileWriteCalls,
		FileWriteBytes: s.fileWriteBytes,
		PartsOpened:    s.partsOpened,
		PartsClosed:    s.partsClosed,
	}
}

func (s *Writer) openNext() error {
	s.seq++
	path := filepath.Clean(s.nameFunc(s.seq))

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("Failed to create target directory: %w. Remedy: Check path validity and write permissions.", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to create part file %q: %w. Remedy: Check target-folder write permissions and free disk space.", path, err)
	}

	s.current = f
	s.written = 0
	s.paths = append(s.paths, path)
	s.partsOpened++
	return nil
}

func (s *Writer) closeCurrent() error {
	if s.current == nil {
		return nil
	}
	err := s.current.Close()
	s.current = nil
	s.partsClosed++
	return err
}

// SequentialReader joins multiple part files into a single io.Reader.
type SequentialReader struct {
	paths   []string
	idx     int
	current *os.File
}

// NewSequentialReader creates a reader that reads parts in order.
func NewSequentialReader(paths []string) *SequentialReader {
	return &SequentialReader{paths: paths}
}

// Read implements io.Reader across all part files.
func (r *SequentialReader) Read(p []byte) (int, error) {
	for {
		if r.current == nil {
			if r.idx >= len(r.paths) {
				return 0, io.EOF
			}
			f, err := os.Open(r.paths[r.idx])
			if err != nil {
				return 0, fmt.Errorf("Failed to open part file %q: %w. Remedy: Check that the part file exists and is readable.", r.paths[r.idx], err)
			}
			r.current = f
			r.idx++
		}

		n, err := r.current.Read(p)
		if err == io.EOF {
			if closeErr := r.current.Close(); closeErr != nil {
				r.current = nil
				return n, fmt.Errorf("Failed to close part file: %w. Remedy: Retry the operation; if it persists, check file-system health.", closeErr)
			}
			r.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		if n == 0 && err == nil {
			currentPath := ""
			if r.idx > 0 && r.idx-1 < len(r.paths) {
				currentPath = r.paths[r.idx-1]
			}
			return 0, fmt.Errorf("No progress while reading part file %q. Remedy: Check drive/network availability and retry.", currentPath)
		}
		return n, err
	}
}

// Close closes any open file handle.
func (r *SequentialReader) Close() error {
	if r.current != nil {
		err := r.current.Close()
		r.current = nil
		return err
	}
	return nil
}
