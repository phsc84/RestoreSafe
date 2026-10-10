package container

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/phsc84/restoresafe/internal/problem"
)

// partsReader presents the part files of a set as one io.ReaderAt. It keeps
// at most one file open, which suits the mostly sequential access pattern.
type partsReader struct {
	paths   []string
	starts  []int64 // logical offset of each part
	size    int64
	openIdx int
	open    *os.File
}

func newPartsReader(paths []string) (*partsReader, error) {
	r := &partsReader{paths: paths, starts: make([]int64, len(paths)), openIdx: -1}
	for i, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, problem.Errorf("Failed to inspect part file %q: %w.", p, err).WithRemedy("Check that the part file exists and is readable.")
		}
		r.starts[i] = r.size
		r.size += fi.Size()
	}
	return r, nil
}

// Size returns the total size of all parts.
func (r *partsReader) Size() int64 { return r.size }

// ReadAt implements io.ReaderAt across the part files.
func (r *partsReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	total := 0
	for len(p) > 0 {
		if off >= r.size {
			return total, io.EOF
		}
		idx := sort.Search(len(r.starts), func(i int) bool { return r.starts[i] > off }) - 1
		f, err := r.file(idx)
		if err != nil {
			return total, err
		}
		n, err := f.ReadAt(p, off-r.starts[idx])
		total += n
		off += int64(n)
		p = p[n:]
		if err != nil && err != io.EOF {
			return total, problem.Errorf("Failed to read part file %q: %w.", r.paths[idx], err).WithRemedy("Check drive/network availability and retry.")
		}
		if n == 0 && err == io.EOF {
			// The part is shorter than when it was measured.
			return total, problem.Errorf("Part file %q changed while reading.", r.paths[idx]).WithRemedy("Do not modify backup files during restore or verify.")
		}
	}
	return total, nil
}

func (r *partsReader) file(idx int) (*os.File, error) {
	if r.openIdx == idx && r.open != nil {
		return r.open, nil
	}
	if r.open != nil {
		r.open.Close() //nolint:errcheck
		r.open = nil
	}
	f, err := os.Open(r.paths[idx])
	if err != nil {
		return nil, problem.Errorf("Failed to open part file %q: %w.", r.paths[idx], err).WithRemedy("Check that the part file exists and is readable.")
	}
	r.open = f
	r.openIdx = idx
	return f, nil
}

// Close closes the open part file, if any.
func (r *partsReader) Close() error {
	if r.open == nil {
		return nil
	}
	err := r.open.Close()
	r.open = nil
	r.openIdx = -1
	return err
}
