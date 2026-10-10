package container

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/format/manifest"
)

// heapPeak samples the live heap every millisecond until stop is called and
// returns the highest value seen.
func heapPeak() (stop func() uint64) {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	read := func() uint64 {
		metrics.Read(sample)
		return sample[0].Value.Uint64()
	}
	var (
		mu   sync.Mutex
		peak = read()
		done = make(chan struct{})
		wg   sync.WaitGroup
	)
	wg.Go(func() {
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				v := read()
				mu.Lock()
				peak = max(peak, v)
				mu.Unlock()
			}
		}
	})
	return func() uint64 {
		close(done)
		wg.Wait()
		return max(peak, read())
	}
}

func liveHeap() uint64 {
	runtime.GC()
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

// TestManifestStreamsAtOneMillionEntries checks SPEC-2.0 section 5.3: the
// serialized manifest is written and read as a stream, never held in memory
// as a whole. At 1,000,000 entries it is about 210 MB. Writing and reading
// each add less than that to the heap on top of the entries: what they add
// is the path map of Manifest.Validate, the growth of the entry slice, and
// garbage between two collections.
func TestManifestStreamsAtOneMillionEntries(t *testing.T) {
	if raceEnabled {
		t.Skip("measures the heap; the race detector changes it and is too slow here")
	}
	const entries = 1_000_000

	ks, master := newPasswordKeySet(t, "pw")
	h, err := NewHeader(manifest.SetTypeFull, "ABC123", "ABC123", "Documents", "2026-10-08", *ks)
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	mb := manifest.NewBuilder(manifest.Header{SetType: h.SetType, ChainID: h.ChainID, DirectoryName: h.DirectoryName, SourcePath: "C:/Users/someone/Documents"})
	offsets := make([]int64, entries)
	for i := range entries {
		offsets[i] = int64(i) * 1024
		mb.Add(manifest.Entry{
			Path: fmt.Sprintf("report-%07d.docx", i), Type: manifest.TypeFile, Size: 1000,
			ModTime: 1_790_000_000_000_000_000, ChangeTime: 1_790_000_000_000_000_000, CreationTime: 1_790_000_000_000_000_000,
			Hash: testHash, Origin: manifest.OriginFull, Offset: &offsets[i],
		})
	}

	dir := t.TempDir()
	sw := NewWriter(func(seq int) string { return filepath.Join(dir, fmt.Sprintf("part-%03d.enc", seq)) }, 1<<30)
	// Collect often, so that the peak shows live memory and not garbage.
	defer debug.SetGCPercent(debug.SetGCPercent(5))
	base := liveHeap()
	stop := heapPeak()
	res, err := Write(sw, h, master, 1<<30, bytes.NewReader(nil), func(w io.Writer) error { return mb.Encode(w) })
	writePeak := stop()
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("close split writer: %v", err)
	}
	limit := uint64(res.Trailer.ManifestLength)
	t.Logf("manifest %d MB; write adds %d MiB at peak", res.Trailer.ManifestLength/1e6, (writePeak-base)>>20)
	if writePeak-base > limit {
		t.Errorf("writing the manifest added %d MiB to the heap, limit %d MiB", (writePeak-base)>>20, limit>>20)
	}

	set, err := Open(sw.Paths())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer set.Close()
	keys, err := DeriveSectionKeys(master, set.HeaderHash)
	if err != nil {
		t.Fatalf("DeriveSectionKeys: %v", err)
	}
	mb, offsets = nil, nil // the parsed manifest replaces the builder's entries
	base = liveHeap()
	stop = heapPeak()
	m, sum, err := set.ReadManifest(keys)
	readPeak := stop()
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	parsed := liveHeap() - base
	t.Logf("parsed manifest %d MiB; read adds %d MiB at peak on top of it", parsed>>20, (readPeak-base-parsed)>>20)
	if len(m.Entries) != entries || sum != res.ManifestSHA256 {
		t.Fatalf("read %d entries with SHA-256 %s, wrote %d with %s", len(m.Entries), sum, entries, res.ManifestSHA256)
	}
	if readPeak-base > parsed+limit {
		t.Errorf("reading the manifest added %d MiB to the heap on top of the parsed manifest, limit %d MiB", (readPeak-base-parsed)>>20, limit>>20)
	}
}
