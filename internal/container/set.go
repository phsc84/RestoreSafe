package container

import (
	"RestoreSafe/internal/manifest"
	"RestoreSafe/internal/security/cryptox"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// Section IDs, part of every chunk's associated data.
const (
	sectionData     = byte(0x01)
	sectionManifest = byte(0x02)
)

// readBufferSize is the read-ahead used when streaming a section from disk.
const readBufferSize = 4 * 1024 * 1024

func sectionAAD(headerHash []byte, section byte) []byte {
	aad := make([]byte, 0, len(headerHash)+1)
	aad = append(aad, headerHash...)
	return append(aad, section)
}

// WriteResult describes a set written by Write.
type WriteResult struct {
	Trailer   Trailer
	TotalSize int64
	// ManifestSHA256 is the hex SHA-256 of the plaintext manifest; a
	// differential records its base's value in base_manifest_sha256.
	ManifestSHA256 string
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

// Write writes a complete set to dst: header, data section (encrypting data
// until EOF), manifest section, and trailer. manifestFn is called after data
// is fully consumed and must return the serialized manifest. splitSize is the
// part size of the split writer behind dst, used to record the part count in
// the trailer (<= 0 means a single part).
func Write(dst io.Writer, h *Header, master []byte, splitSize int64, data io.Reader, manifestFn func() ([]byte, error)) (*WriteResult, error) {
	encoded, err := h.Encode()
	if err != nil {
		return nil, err
	}
	headerHash := HashHeader(encoded)
	keys, err := DeriveSectionKeys(master, headerHash)
	if err != nil {
		return nil, err
	}
	defer keys.Zero()

	cw := &countingWriter{w: dst}
	if _, err := cw.Write(encoded); err != nil {
		return nil, fmt.Errorf("Failed to write backup header: %w", err)
	}

	dataOffset := cw.n
	if err := cryptox.EncryptStream(cw, data, keys.Data, sectionAAD(headerHash, sectionData)); err != nil {
		return nil, err
	}
	dataLength := cw.n - dataOffset

	manifestBytes, err := manifestFn()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(manifestBytes)
	manifestOffset := cw.n
	if err := cryptox.EncryptStream(cw, bytes.NewReader(manifestBytes), keys.Manifest, sectionAAD(headerHash, sectionManifest)); err != nil {
		return nil, fmt.Errorf("Failed to write backup manifest: %w", err)
	}
	manifestLength := cw.n - manifestOffset

	total := cw.n + TrailerLen
	parts := int64(1)
	if splitSize > 0 {
		parts = (total + splitSize - 1) / splitSize
	}
	trailer := Trailer{
		DataOffset:     dataOffset,
		DataLength:     dataLength,
		ManifestOffset: manifestOffset,
		ManifestLength: manifestLength,
		PartCount:      int(parts),
	}
	if _, err := cw.Write(trailer.Encode()); err != nil {
		return nil, fmt.Errorf("Failed to write backup trailer: %w", err)
	}
	return &WriteResult{Trailer: trailer, TotalSize: cw.n, ManifestSHA256: hex.EncodeToString(sum[:])}, nil
}

// Set is an opened, structurally checked backup set.
type Set struct {
	Paths      []string
	Header     *Header
	HeaderHash []byte
	Trailer    Trailer
	parts      *partsReader
}

// Open reads the header and trailer of the set formed by paths (in part
// order) and checks the set's structure. It needs no key. A set whose trailer
// is missing or inconsistent returns an *ErrIncomplete error.
func Open(paths []string) (*Set, error) {
	if len(paths) == 0 {
		return nil, incompleteErr("no part files found")
	}
	pr, err := newPartsReader(paths)
	if err != nil {
		return nil, err
	}
	s, err := openParts(pr, paths)
	if err != nil {
		pr.Close() //nolint:errcheck
		return nil, err
	}
	return s, nil
}

func openParts(pr *partsReader, paths []string) (*Set, error) {
	h, encoded, err := ReadHeader(io.NewSectionReader(pr, 0, pr.Size()))
	if err != nil {
		return nil, err
	}
	if pr.Size() < int64(len(encoded))+TrailerLen {
		return nil, incompleteErr("file is too short")
	}
	raw := make([]byte, TrailerLen)
	if _, err := pr.ReadAt(raw, pr.Size()-TrailerLen); err != nil {
		return nil, err
	}
	trailer, err := DecodeTrailer(raw)
	if err != nil {
		return nil, err
	}
	if err := trailer.check(int64(len(encoded)), pr.Size(), len(paths)); err != nil {
		return nil, err
	}
	return &Set{Paths: paths, Header: h, HeaderHash: HashHeader(encoded), Trailer: trailer, parts: pr}, nil
}

// Close releases the open part file handle.
func (s *Set) Close() error {
	if s == nil || s.parts == nil {
		return nil
	}
	return s.parts.Close()
}

// SectionKeys derives this set's section keys from the key set master key.
func (s *Set) SectionKeys(master []byte) (*SectionKeys, error) {
	return DeriveSectionKeys(master, s.HeaderHash)
}

func (s *Set) sectionReader(offset, length int64) io.Reader {
	return bufio.NewReaderSize(io.NewSectionReader(s.parts, offset, length), readBufferSize)
}

// ReadManifest decrypts, parses, and validates the manifest section and checks
// it against the set header. It also returns the hex SHA-256 of the plaintext
// manifest.
func (s *Set) ReadManifest(keys *SectionKeys) (*manifest.Manifest, string, error) {
	var buf bytes.Buffer
	err := cryptox.DecryptStream(&buf, s.sectionReader(s.Trailer.ManifestOffset, s.Trailer.ManifestLength), keys.Manifest, sectionAAD(s.HeaderHash, sectionManifest))
	if err != nil {
		return nil, "", sectionErr("manifest", err)
	}
	m, err := manifest.Parse(buf.Bytes())
	if err != nil {
		return nil, "", err
	}
	h := s.Header
	mh := m.Header
	if mh.SetType != h.SetType || mh.ChainID != h.ChainID || mh.DiffNumber != h.DiffNumber || mh.DirectoryName != h.DirectoryName {
		return nil, "", fmt.Errorf("Backup manifest does not match its set header. Remedy: Use an unmodified backup created by RestoreSafe.")
	}
	sum := sha256.Sum256(buf.Bytes())
	return m, hex.EncodeToString(sum[:]), nil
}

// DecryptData streams the decrypted data section (a TAR stream) to dst.
func (s *Set) DecryptData(keys *SectionKeys, dst io.Writer) error {
	err := cryptox.DecryptStream(dst, s.sectionReader(s.Trailer.DataOffset, s.Trailer.DataLength), keys.Data, sectionAAD(s.HeaderHash, sectionData))
	if err != nil {
		return sectionErr("data", err)
	}
	return nil
}

func sectionErr(section string, err error) error {
	if errors.Is(err, cryptox.ErrCorrupted) {
		return fmt.Errorf("%w in the %s section. Remedy: The backup files were damaged or modified; use another backup or a copy of these files.", cryptox.ErrCorrupted, section)
	}
	return err
}
