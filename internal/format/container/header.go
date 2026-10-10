// Package container implements RestoreSafe container format 2: the on-disk
// layout of one backup set (all part files of one source directory in one
// run).
//
// The part files, concatenated in order, form one logical stream:
//
//	set header        plaintext JSON, authenticated as AAD of every chunk
//	data section      encrypted chunk stream, plaintext = TAR of file contents
//	manifest section  encrypted chunk stream, plaintext = manifest (JSON Lines)
//	trailer           64 bytes, locates the sections and marks the set complete
//
// Keys come from the key set stored in the header (see keyset.go): a random
// master key wrapped once per unlock method ("slot"). Each set derives its own
// data and manifest keys from the master key and the header hash.
package container

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/phsc84/restoresafe/internal/buildinfo"
	"github.com/phsc84/restoresafe/internal/format/manifest"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
)

const (
	magicPrefix = "RSBKP\x00"
	// FormatVersion is the container format written by this RestoreSafe version.
	FormatVersion = byte(2)
	// headerPrefixLen is magic (6) + version (1) + reserved (1) + JSON length (4).
	headerPrefixLen = 12
	maxHeaderJSON   = 64 * 1024
	// CompressionNone is the only compression value defined in format 2.
	CompressionNone = "none"
	setNonceLen     = 32
)

var (
	idPattern   = regexp.MustCompile(`^[A-Z0-9]{6}$`)
	datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Header is the plaintext set header.
type Header struct {
	SetType            string `json:"set_type"`
	ChainID            string `json:"chain_id"`
	DiffNumber         int    `json:"diff_number,omitempty"`
	RunID              string `json:"run_id"`
	SetNonce           []byte `json:"set_nonce"`
	DirectoryName      string `json:"directory_name"`
	Date               string `json:"date"`
	CreatedUTC         string `json:"created_utc"`
	AppVersion         string `json:"app_version"`
	BaseDate           string `json:"base_date,omitempty"`
	BaseManifestSHA256 string `json:"base_manifest_sha256,omitempty"`
	ChunkSize          uint32 `json:"chunk_size"`
	Compression        string `json:"compression"`
	KeySet             KeySet `json:"key_set"`
}

// IsDiff reports whether the header describes a differential backup.
func (h *Header) IsDiff() bool { return h.SetType == manifest.SetTypeDiff }

// Created returns CreatedUTC parsed as a time. Validate guarantees it parses.
func (h *Header) Created() time.Time {
	t, _ := time.Parse(time.RFC3339, h.CreatedUTC)
	return t
}

// NewHeader returns a header with the per-set fields (nonce, creation time,
// app version, chunk size, compression) filled in.
func NewHeader(setType, chainID, runID, directoryName, date string, keySet KeySet) (*Header, error) {
	nonce, err := cryptox.RandomBytes(setNonceLen)
	if err != nil {
		return nil, err
	}
	return &Header{
		SetType:       setType,
		ChainID:       chainID,
		RunID:         runID,
		SetNonce:      nonce,
		DirectoryName: directoryName,
		Date:          date,
		CreatedUTC:    time.Now().UTC().Format(time.RFC3339Nano),
		AppVersion:    buildinfo.Version,
		ChunkSize:     cryptox.ChunkSize,
		Compression:   CompressionNone,
		KeySet:        keySet,
	}, nil
}

// Validate checks every header field.
func (h *Header) Validate() error {
	switch h.SetType {
	case manifest.SetTypeFull:
		if h.DiffNumber != 0 || h.BaseDate != "" || h.BaseManifestSHA256 != "" {
			return headerErr("full backup header has differential fields")
		}
	case manifest.SetTypeDiff:
		if h.DiffNumber < 1 || h.DiffNumber > 999 {
			return headerErr("invalid differential number %d", h.DiffNumber)
		}
		if !datePattern.MatchString(h.BaseDate) {
			return headerErr("invalid base date %q", h.BaseDate)
		}
		if len(h.BaseManifestSHA256) != 64 {
			return headerErr("invalid base manifest hash")
		}
	default:
		return headerErr("unknown set type %q", h.SetType)
	}
	if !idPattern.MatchString(h.ChainID) || !idPattern.MatchString(h.RunID) {
		return headerErr("invalid chain or run ID")
	}
	if len(h.SetNonce) != setNonceLen {
		return headerErr("invalid set nonce")
	}
	if err := naming.ValidateBackupEntryName(h.DirectoryName); err != nil {
		return err
	}
	if !datePattern.MatchString(h.Date) {
		return headerErr("invalid date %q", h.Date)
	}
	if _, err := time.Parse(time.RFC3339, h.CreatedUTC); err != nil {
		return headerErr("invalid creation time %q", h.CreatedUTC)
	}
	if h.ChunkSize != cryptox.ChunkSize {
		return headerErr("unsupported chunk size %d", h.ChunkSize)
	}
	if h.Compression != CompressionNone {
		return problem.Errorf("Backup uses compression %q, which this RestoreSafe version does not support.", h.Compression).WithRemedy("Use the newer RestoreSafe version that created this backup: https://github.com/phsc84/RestoreSafe/releases")
	}
	return h.KeySet.Validate()
}

func headerErr(format string, args ...any) error {
	return problem.Errorf("Invalid backup header: %s.", fmt.Sprintf(format, args...)).WithRemedy("Use an unmodified backup created by RestoreSafe.")
}

// Encode validates the header and returns its on-disk bytes.
func (h *Header) Encode() ([]byte, error) {
	if err := h.Validate(); err != nil {
		return nil, fmt.Errorf("Internal error: refusing to write invalid header: %w", err)
	}
	body, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("Failed to encode backup header: %w", err)
	}
	if len(body) > maxHeaderJSON {
		return nil, fmt.Errorf("Backup header too large (%d bytes).", len(body))
	}
	buf := make([]byte, headerPrefixLen, headerPrefixLen+len(body))
	copy(buf, magicPrefix)
	buf[6] = FormatVersion
	buf[7] = 0
	binary.BigEndian.PutUint32(buf[8:12], uint32(len(body)))
	return append(buf, body...), nil
}

// HashHeader returns the SHA-256 of the complete encoded header.
func HashHeader(encoded []byte) []byte {
	sum := sha256.Sum256(encoded)
	return sum[:]
}

// ReadHeader reads and validates a header from r. It returns the header and
// its raw encoded bytes (needed for the header hash).
func ReadHeader(r io.Reader) (*Header, []byte, error) {
	prefix := make([]byte, headerPrefixLen)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return nil, nil, problem.Errorf("Failed to read backup header: %w.", err).WithRemedy("Check that the backup file is complete and readable.")
	}
	if string(prefix[:len(magicPrefix)]) != magicPrefix {
		return nil, nil, problem.New("Invalid file format (not a RestoreSafe backup).").WithRemedy("Select a valid RestoreSafe .enc backup file.")
	}
	if version := prefix[6]; version != FormatVersion {
		if version < FormatVersion {
			return nil, nil, problem.Errorf("Backup was created by RestoreSafe 1.x (backup format %d) and cannot be read by RestoreSafe 2.", version).WithRemedy("Restore it with RestoreSafe 1.0.2: https://github.com/phsc84/RestoreSafe/releases")
		}
		return nil, nil, problem.Errorf("Backup format %d is newer than this RestoreSafe version supports (format %d).", version, FormatVersion).WithRemedy("Use the newer RestoreSafe version that created this backup: https://github.com/phsc84/RestoreSafe/releases")
	}
	if prefix[7] != 0 {
		return nil, nil, headerErr("reserved byte is not zero")
	}
	length := binary.BigEndian.Uint32(prefix[8:12])
	if length == 0 || length > maxHeaderJSON {
		return nil, nil, headerErr("invalid header length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, nil, problem.Errorf("Failed to read backup header: %w.", err).WithRemedy("Check that the backup file is complete and readable.")
	}

	var h Header
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return nil, nil, headerErr("%v", err)
	}
	if dec.More() {
		return nil, nil, headerErr("trailing data after header JSON")
	}
	if err := h.Validate(); err != nil {
		return nil, nil, err
	}
	return &h, append(prefix, body...), nil
}
