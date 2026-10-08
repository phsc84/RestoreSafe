// Package manifest defines the backup manifest: the list of every file and
// directory of a source directory at backup time, stored encrypted inside each
// backup set (container format 2).
//
// The manifest is JSON Lines: one header object, one object per entry in walk
// order, and a footer object as the last line. It records a SHA-256 per file so
// every restore and verify checks the restored content end to end, and it
// records where each file's content lives (in the chain's full backup or in
// this differential), which lets a differential restore reproduce the exact
// source state including deletions.
package manifest

import (
	"RestoreSafe/internal/problem"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// Version is the manifest format version written by this RestoreSafe version.
const Version = 1

// HashAlg is the content hash algorithm recorded in the manifest header.
const HashAlg = "sha256"

// Set types.
const (
	SetTypeFull = "full"
	SetTypeDiff = "diff"
)

// Entry types.
const (
	TypeFile    = "f"
	TypeDir     = "d"
	TypeSkipped = "s"
)

// Origins of file content.
const (
	OriginFull = "F" // data section of the chain's full backup
	OriginDiff = "D" // data section of this differential
)

// Attribute bits restored by RestoreSafe (Windows FILE_ATTRIBUTE_* values).
const (
	AttrReadOnly uint32 = 0x1
	AttrHidden   uint32 = 0x2
	AttrSystem   uint32 = 0x4
	// AttrMask is the set of attribute bits a manifest may contain.
	AttrMask = AttrReadOnly | AttrHidden | AttrSystem
)

// maxLineBytes bounds a single manifest line (long paths and error texts).
const maxLineBytes = 1 << 20

// Header is the first line of a manifest.
type Header struct {
	ManifestVersion int      `json:"manifest_version"`
	SetType         string   `json:"set_type"`
	ChainID         string   `json:"chain_id"`
	DiffNumber      int      `json:"diff_number,omitempty"`
	DirectoryName   string   `json:"directory_name"`
	SourcePath      string   `json:"source_path"`
	HashAlg         string   `json:"hash_alg"`
	Exclude         []string `json:"exclude"`
}

// Entry is one file, directory, or skipped file. Times are UTC nanoseconds
// since the Unix epoch.
type Entry struct {
	Path         string `json:"p"`
	Type         string `json:"t"`
	Size         int64  `json:"s,omitempty"`
	ModTime      int64  `json:"m,omitempty"`
	ChangeTime   int64  `json:"c,omitempty"`
	CreationTime int64  `json:"b,omitempty"`
	Attributes   uint32 `json:"a,omitempty"`
	Hash         string `json:"h,omitempty"`
	Origin       string `json:"o,omitempty"`
	// Offset is the byte offset of the file's TAR header in this set's
	// plaintext TAR stream. Present only when the content is in this set.
	Offset *int64 `json:"off,omitempty"`
	// Stale marks a file that could not be read in this run; the entry refers
	// to the older content in the full backup.
	Stale bool `json:"x,omitempty"`
	// Void marks that this set's data section contains an unusable TAR entry
	// for this path (the read failed after its header was written) that
	// restore must skip.
	Void bool `json:"v,omitempty"`
	// Reason is the error text for skipped files.
	Reason string `json:"r,omitempty"`
}

// HasContentInSet reports whether the entry's content is stored in the data
// section of the set this manifest belongs to.
func (e Entry) HasContentInSet() bool {
	return e.Type == TypeFile && e.Offset != nil
}

// Footer is the last line of a manifest.
type Footer struct {
	End        bool  `json:"end"`
	Entries    int   `json:"entries"`
	Files      int   `json:"files"`
	Dirs       int   `json:"dirs"`
	Skipped    int   `json:"skipped"`
	Stale      int   `json:"stale"`
	TotalBytes int64 `json:"total_bytes"`
	DataBytes  int64 `json:"data_bytes"`
}

// Manifest is a parsed and validated manifest.
type Manifest struct {
	Header  Header
	Entries []Entry
	Footer  Footer
}

// Builder collects manifest entries during a backup and serializes them.
type Builder struct {
	header  Header
	entries []Entry
}

// NewBuilder starts a manifest with the given header. ManifestVersion,
// HashAlg, and a nil Exclude list are filled in automatically.
func NewBuilder(h Header) *Builder {
	h.ManifestVersion = Version
	h.HashAlg = HashAlg
	if h.Exclude == nil {
		h.Exclude = []string{}
	}
	return &Builder{header: h}
}

// Add appends an entry in walk order.
func (b *Builder) Add(e Entry) {
	b.entries = append(b.entries, e)
}

// SkipLastDirectory turns the most recently added entry into a skipped entry
// when it is the directory p. The walk records a directory before it lists
// its content, so a directory that turns out to be unreadable is converted
// here. It reports whether the entry was converted.
func (b *Builder) SkipLastDirectory(p, reason string) bool {
	if len(b.entries) == 0 {
		return false
	}
	last := &b.entries[len(b.entries)-1]
	if last.Path != p || last.Type != TypeDir {
		return false
	}
	*last = Entry{Path: p, Type: TypeSkipped, Reason: reason}
	return true
}

// Footer returns the totals of the entries collected so far.
func (b *Builder) Footer() Footer { return computeFooter(b.entries) }

// Bytes computes the footer, validates the manifest, and returns its
// serialized form. Validation here guarantees RestoreSafe never writes a
// manifest it would refuse to read.
func (b *Builder) Bytes() ([]byte, error) {
	m := &Manifest{Header: b.header, Entries: b.entries, Footer: computeFooter(b.entries)}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("Internal error: generated manifest is invalid: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m.Header); err != nil {
		return nil, fmt.Errorf("Failed to encode manifest header: %w", err)
	}
	for _, e := range m.Entries {
		if err := enc.Encode(e); err != nil {
			return nil, fmt.Errorf("Failed to encode manifest entry %q: %w", e.Path, err)
		}
	}
	if err := enc.Encode(m.Footer); err != nil {
		return nil, fmt.Errorf("Failed to encode manifest footer: %w", err)
	}
	return buf.Bytes(), nil
}

func computeFooter(entries []Entry) Footer {
	f := Footer{End: true, Entries: len(entries)}
	for _, e := range entries {
		switch e.Type {
		case TypeFile:
			f.Files++
			f.TotalBytes += e.Size
			if e.HasContentInSet() {
				f.DataBytes += e.Size
			}
			if e.Stale {
				f.Stale++
			}
		case TypeDir:
			f.Dirs++
		case TypeSkipped:
			f.Skipped++
		}
	}
	return f
}

// Parse reads and validates a serialized manifest.
func Parse(data []byte) (*Manifest, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	m := &Manifest{}
	lineNo := 0
	sawHeader := false
	sawFooter := false
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if sawFooter {
			return nil, manifestErr("unexpected data after the footer (line %d)", lineNo)
		}
		switch {
		case !sawHeader:
			if err := decodeStrict(line, &m.Header); err != nil {
				return nil, manifestErr("invalid header line: %v", err)
			}
			sawHeader = true
		case bytes.HasPrefix(line, []byte(`{"end":`)):
			if err := decodeStrict(line, &m.Footer); err != nil {
				return nil, manifestErr("invalid footer line: %v", err)
			}
			sawFooter = true
		default:
			var e Entry
			if err := decodeStrict(line, &e); err != nil {
				return nil, manifestErr("invalid entry on line %d: %v", lineNo, err)
			}
			m.Entries = append(m.Entries, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, manifestErr("failed to read: %v", err)
	}
	if !sawHeader {
		return nil, manifestErr("empty manifest")
	}
	if !sawFooter {
		return nil, manifestErr("missing footer (manifest is truncated)")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeStrict(line []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func manifestErr(format string, args ...any) error {
	return problem.Errorf("Invalid backup manifest: %s.", fmt.Sprintf(format, args...)).WithRemedy("Use an unmodified backup created by RestoreSafe.")
}

// Validate checks the manifest against the rules of the format (2.0 spec 5.2).
// Consistency with the set header is checked by the caller.
func (m *Manifest) Validate() error {
	h := m.Header
	if h.ManifestVersion != Version {
		return problem.Errorf("Unsupported backup manifest version %d.", h.ManifestVersion).WithRemedy("Use the RestoreSafe version that created this backup.")
	}
	if h.HashAlg != HashAlg {
		return manifestErr("unsupported hash algorithm %q", h.HashAlg)
	}
	isDiff := false
	switch h.SetType {
	case SetTypeFull:
		if h.DiffNumber != 0 {
			return manifestErr("full backup with differential number %d", h.DiffNumber)
		}
	case SetTypeDiff:
		isDiff = true
		if h.DiffNumber < 1 || h.DiffNumber > 999 {
			return manifestErr("invalid differential number %d", h.DiffNumber)
		}
	default:
		return manifestErr("unknown set type %q", h.SetType)
	}
	if h.ChainID == "" || h.DirectoryName == "" {
		return manifestErr("missing chain ID or directory name")
	}

	seen := make(map[string]bool, len(m.Entries))
	seenFolded := make(map[string]string, len(m.Entries))
	dirs := make(map[string]bool)
	for _, e := range m.Entries {
		if err := ValidatePath(e.Path); err != nil {
			return err
		}
		if seen[e.Path] {
			return manifestErr("duplicate path %q", e.Path)
		}
		folded := strings.ToLower(e.Path)
		if other, ok := seenFolded[folded]; ok {
			return manifestErr("paths %q and %q differ only in case and would collide on Windows", other, e.Path)
		}
		seen[e.Path] = true
		seenFolded[folded] = e.Path

		if parent := path.Dir(e.Path); parent != "." && !dirs[parent] {
			return manifestErr("entry %q appears before its parent directory", e.Path)
		}
		if err := validateEntry(e, isDiff); err != nil {
			return err
		}
		if e.Type == TypeDir {
			dirs[e.Path] = true
		}
	}

	want := computeFooter(m.Entries)
	if m.Footer != want {
		return manifestErr("footer totals do not match the entries")
	}
	return nil
}

func validateEntry(e Entry, isDiff bool) error {
	if e.Attributes&^AttrMask != 0 {
		return manifestErr("unknown attribute bits 0x%x on %q", e.Attributes, e.Path)
	}
	switch e.Type {
	case TypeDir:
		if e.Size != 0 || e.Hash != "" || e.Origin != "" || e.Offset != nil || e.Stale || e.Void || e.Reason != "" {
			return manifestErr("directory %q has file fields", e.Path)
		}
	case TypeSkipped:
		if e.Reason == "" {
			return manifestErr("skipped file %q has no reason", e.Path)
		}
		if e.Size != 0 || e.Hash != "" || e.Origin != "" || e.Offset != nil || e.Stale {
			return manifestErr("skipped file %q has content fields", e.Path)
		}
	case TypeFile:
		if e.Size < 0 {
			return manifestErr("negative size on %q", e.Path)
		}
		if !isValidHash(e.Hash) {
			return manifestErr("invalid hash on %q", e.Path)
		}
		if e.Reason != "" {
			return manifestErr("file %q has a skip reason", e.Path)
		}
		if e.Offset != nil && *e.Offset < 0 {
			return manifestErr("negative offset on %q", e.Path)
		}
		switch e.Origin {
		case OriginFull:
			// In a full backup the content is always in this set; in a
			// differential an "F" entry refers to the full's data section.
			if !isDiff && (e.Offset == nil || e.Void) {
				return manifestErr("file %q has no content in the full backup", e.Path)
			}
			if isDiff && e.Offset != nil {
				return manifestErr("file %q refers to the full backup but has an offset in this differential", e.Path)
			}
		case OriginDiff:
			if !isDiff {
				return manifestErr("file %q has differential origin in a full backup", e.Path)
			}
			if e.Offset == nil || e.Void || e.Stale {
				return manifestErr("file %q has invalid differential content fields", e.Path)
			}
		default:
			return manifestErr("invalid origin %q on %q", e.Origin, e.Path)
		}
		if e.Stale && (!isDiff || e.Origin != OriginFull) {
			return manifestErr("stale marker on %q is only valid for differential entries that refer to the full backup", e.Path)
		}
		if e.Void && !e.Stale {
			return manifestErr("void marker on file %q without stale marker", e.Path)
		}
	default:
		return manifestErr("unknown entry type %q on %q", e.Type, e.Path)
	}
	return nil
}

func isValidHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidatePath checks that p is a clean, relative, forward-slash path that
// cannot escape the restore directory.
func ValidatePath(p string) error {
	invalid := func(reason string) error {
		return problem.Errorf("Invalid path in backup (%s): %q.", reason, p).WithRemedy("Do not use this backup; use only unmodified, trusted backup files.")
	}
	if strings.TrimSpace(p) == "" {
		return invalid("empty")
	}
	if strings.ContainsAny(p, "\\:\x00") {
		return invalid("backslash, drive marker, or NUL")
	}
	if strings.HasPrefix(p, "/") {
		return invalid("absolute path")
	}
	if path.Clean(p) != p {
		return invalid("not a clean path")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return invalid("path traversal")
		}
	}
	return nil
}
