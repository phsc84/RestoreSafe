package container

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/phsc84/restoresafe/internal/problem"
)

// TrailerLen is the fixed trailer size at the end of every set.
const TrailerLen = 64

var trailerMagic = []byte("RSTRL\x00\x02\x00")

// minSectionLen is the smallest valid encrypted section: one empty final
// chunk (5 bytes framing + 16 bytes GCM tag).
const minSectionLen = 21

// Trailer locates the encrypted sections and records the part count. It is a
// locator and completeness marker, not a security boundary: tampering can only
// point to bytes that then fail authentication.
type Trailer struct {
	DataOffset     int64
	DataLength     int64
	ManifestOffset int64
	ManifestLength int64
	PartCount      int
}

// Encode returns the 64-byte on-disk trailer.
func (t Trailer) Encode() []byte {
	buf := make([]byte, 0, TrailerLen)
	buf = append(buf, trailerMagic...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(t.DataOffset))
	buf = binary.BigEndian.AppendUint64(buf, uint64(t.DataLength))
	buf = binary.BigEndian.AppendUint64(buf, uint64(t.ManifestOffset))
	buf = binary.BigEndian.AppendUint64(buf, uint64(t.ManifestLength))
	buf = binary.BigEndian.AppendUint32(buf, uint32(t.PartCount))
	buf = binary.BigEndian.AppendUint32(buf, 0)
	sum := sha256.Sum256(buf)
	return append(buf, sum[:16]...)
}

// DecodeTrailer parses and checksums a trailer.
func DecodeTrailer(b []byte) (Trailer, error) {
	if len(b) != TrailerLen {
		return Trailer{}, incompleteErr("trailer has wrong length")
	}
	if !bytes.Equal(b[:8], trailerMagic) {
		return Trailer{}, incompleteErr("trailer not found")
	}
	sum := sha256.Sum256(b[:48])
	if !bytes.Equal(sum[:16], b[48:64]) {
		return Trailer{}, incompleteErr("trailer checksum mismatch")
	}
	if binary.BigEndian.Uint32(b[44:48]) != 0 {
		return Trailer{}, incompleteErr("trailer reserved field is not zero")
	}
	t := Trailer{
		DataOffset:     int64(binary.BigEndian.Uint64(b[8:16])),
		DataLength:     int64(binary.BigEndian.Uint64(b[16:24])),
		ManifestOffset: int64(binary.BigEndian.Uint64(b[24:32])),
		ManifestLength: int64(binary.BigEndian.Uint64(b[32:40])),
		PartCount:      int(binary.BigEndian.Uint32(b[40:44])),
	}
	return t, nil
}

// check verifies the trailer against the header length, the total stream size,
// and the number of part files present.
func (t Trailer) check(headerLen, totalSize int64, partsPresent int) error {
	switch {
	case t.DataOffset != headerLen:
		return incompleteErr("data section does not start after the header")
	case t.DataLength < minSectionLen || t.ManifestLength < minSectionLen:
		return incompleteErr("section too short")
	case t.ManifestOffset != t.DataOffset+t.DataLength:
		return incompleteErr("sections are not adjacent")
	case t.ManifestOffset+t.ManifestLength+TrailerLen != totalSize:
		return incompleteErr("size of the part files does not match the trailer")
	case t.PartCount != partsPresent:
		return incompleteErr(fmt.Sprintf("trailer expects %d part file(s), found %d", t.PartCount, partsPresent))
	}
	return nil
}

// ErrIncomplete marks a set whose trailer is missing or inconsistent: the
// backup was interrupted, or part files are missing or damaged.
type ErrIncomplete struct{ Reason string }

func (e *ErrIncomplete) Error() string { return "Backup set is incomplete: " + e.Reason }

// incompleteErr returns the error of an incomplete set, with what the user
// does about it; errors.As finds the *ErrIncomplete in it.
func incompleteErr(reason string) error {
	return problem.Errorf("%w.", &ErrIncomplete{Reason: reason}).
		WithRemedy("Make sure all .enc part files of this backup are present and unmodified, or create a new backup.")
}
