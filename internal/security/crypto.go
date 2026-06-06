// Package crypto provides password-based AES-256-GCM streaming encryption.
//
// # Design decisions
//
// Algorithm: AES-256-GCM
//   - Industry standard authenticated encryption (AEAD)
//   - Provides both confidentiality AND integrity/authenticity
//   - Detects tampering or wrong passwords at decryption time
//
// KDF: Argon2id (RFC 9106)
//   - Winner of the Password Hashing Competition (2015)
//   - Memory-hard → resistant to GPU/ASIC brute-force
//   - "id" variant combines side-channel and GPU resistance
//   - Default parameters: 512 MB memory, 3 iterations, 4 threads
//   - Parameters are configurable and stored in the file header
//
// Stream chunking
//   - Large files are split into fixed-size chunks (chunkSize = 8 MB)
//   - Each chunk gets its own nonce derived deterministically from the
//     chunk index, preventing nonce reuse across chunks of the same stream
//   - A 1-byte flag field and 4-byte big-endian length prefix are written before
//     each encrypted chunk
//   - The chunk index and flags are GCM associated data, binding chunk order and
//     the final-chunk marker to authentication
//   - This avoids temp files and limits RAM usage to ~2× chunkSize
//
// File header layout (all values big-endian), format version 1:
//
//	[6]  magic prefix "RSBKP\x00"
//	[1]  format version (currently 1)
//	[1]  reserved (0x00)
//	[4]  salt length (always saltLen = 32)
//	[32] salt
//	[4]  chunk size (bytes, currently 8388608)
//	[4]  Argon2id time (iterations)
//	[4]  Argon2id memory (kibibytes)
//	[4]  Argon2id threads
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	// magicPrefix is the 6-byte file identifier: ASCII "RSBKP" + null separator.
	magicPrefix = "RSBKP\x00"
	// formatVersion is incremented on any breaking change to the header or chunk layout.
	// Readers that encounter a different version emit a clear version-mismatch error.
	formatVersion = byte(1)
	// saltLen is the byte length of the Argon2id salt.
	saltLen = 32
	// keyLen is the AES-256 key length in bytes.
	keyLen = 32
	// nonceLen is the GCM nonce length.
	nonceLen = 12
	// chunkSize is the plaintext chunk size for streaming.
	chunkSize = 8 * 1024 * 1024 // 8 MB
	// maxEncryptedChunkSize is the largest valid GCM-sealed chunk payload.
	maxEncryptedChunkSize = chunkSize + 16
	// chunkFlagFinal marks the final authenticated chunk in a stream.
	chunkFlagFinal = byte(1)
)

// Argon2id parameter bounds. This package owns the canonical bounds because it
// owns the key-derivation function (argon2.IDKey) and the on-disk header format;
// package util derives its MB-based config bounds from these. Memory is
// expressed here in KiB to match the header and KDF native units.
//
// readHeader enforces these on every backup header so that hostile or corrupted
// values cannot trigger an OOM (a multi-terabyte memory request) or a panic
// (parallelism 0 via the uint8 truncation). The threads maximum also keeps the
// value within the uint8 range argon2 requires.
const (
	MinArgonTime     = 2
	MaxArgonTime     = 20
	MinArgonMemoryKB = 64 * 1024   // 64 MiB
	MaxArgonMemoryKB = 4096 * 1024 // 4096 MiB
	MinArgonThreads  = 1
	MaxArgonThreads  = 255
)

// Argon2Params holds the Argon2id key-derivation parameters.
// All three values are stored in the file header so that decryption always
// uses the exact parameters that were in effect during encryption.
type Argon2Params struct {
	Time     uint32 // number of iterations (passes over memory)
	MemoryKB uint32 // working memory in kibibytes
	Threads  uint8  // degree of parallelism
}

// DefaultArgon2Params are RestoreSafe's default key-derivation parameters:
// 512 MB of memory, 3 iterations, 4 parallel threads. This is well above the
// OWASP Argon2id minimums. These values are the single source of truth for the
// defaults applied when config.yaml omits the argon2 settings.
var DefaultArgon2Params = Argon2Params{
	Time:     3,
	MemoryKB: 512 * 1024,
	Threads:  4,
}

// magic is the full 8-byte header marker: prefix + version + reserved byte.
// Derived from magicPrefix + formatVersion so that bumping formatVersion
// automatically updates the on-disk marker without a manual string edit.
var magic = magicPrefix + string([]byte{formatVersion, 0x00})

// ErrWrongPassword is returned when decryption authentication fails.
var ErrWrongPassword = errors.New("Wrong password or corrupted file")

// deriveKey derives a 256-bit AES key from the password and salt using Argon2id.
func deriveKey(password, salt []byte, params Argon2Params) []byte {
	return argon2.IDKey(password, salt, params.Time, params.MemoryKB, params.Threads, keyLen)
}

// validateArgon2Params reports whether the Argon2id parameters fall within the
// bounds this package enforces before handing them to argon2.IDKey. Both Encrypt
// (in-memory params from config or internal callers) and readHeader (params read
// from an untrusted on-disk header) call it so a single set of checks guards
// against an OOM (multi-terabyte memory request) or a panic (parallelism 0).
//
// Values are taken as uint32 so the raw header fields can be validated before the
// uint8 truncation of Threads: a stored thread count of 256 truncates to 0, so
// validating the post-truncation value would mask it. context and remedy are
// interpolated so the message reads naturally for both the header and the
// encryption-parameter path.
func validateArgon2Params(time, memoryKB, threads uint32, context, remedy string) error {
	switch {
	case time < MinArgonTime || time > MaxArgonTime:
		return fmt.Errorf("Invalid Argon2 time %s: %d (allowed %d-%d). %s", context, time, MinArgonTime, MaxArgonTime, remedy)
	case memoryKB < MinArgonMemoryKB || memoryKB > MaxArgonMemoryKB:
		return fmt.Errorf("Invalid Argon2 memory %s: %d KiB (allowed %d-%d). %s", context, memoryKB, MinArgonMemoryKB, MaxArgonMemoryKB, remedy)
	case threads < MinArgonThreads || threads > MaxArgonThreads:
		return fmt.Errorf("Invalid Argon2 threads %s: %d (allowed %d-%d). %s", context, threads, MinArgonThreads, MaxArgonThreads, remedy)
	}
	return nil
}

// Encrypt reads plaintext from src, encrypts it with password and params, and writes
// ciphertext to dst. The function streams data in chunkSize chunks so that
// arbitrarily large files can be processed with constant memory.
func Encrypt(dst io.Writer, src io.Reader, password []byte, params Argon2Params) error {
	// Validate before doing any work so invalid parameters from internal callers
	// or tests cannot reach argon2.IDKey and trigger an OOM or panic.
	if err := validateArgon2Params(params.Time, params.MemoryKB, uint32(params.Threads), "in encryption parameters", "Remedy: Adjust the argon2 settings in config.yaml."); err != nil {
		return err
	}

	// Generate a random salt.
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("Failed to generate salt: %w", err)
	}

	key := deriveKey(password, salt, params)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("Failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("Failed to create GCM: %w", err)
	}

	// Write file header.
	if err := writeHeader(dst, salt, params); err != nil {
		return err
	}

	// Stream plaintext in chunks.
	buf := make([]byte, chunkSize)
	var chunkIndex uint64

	for {
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("Failed to read plaintext: %w", readErr)
		}

		isFinal := errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF)
		if n == 0 && errors.Is(readErr, io.EOF) {
			if err := writeEncryptedChunk(dst, gcm, chunkIndex, nil, true); err != nil {
				return err
			}
			break
		}

		if err := writeEncryptedChunk(dst, gcm, chunkIndex, buf[:n], isFinal); err != nil {
			return err
		}

		chunkIndex++
		if isFinal {
			break
		}
	}

	return nil
}

// Decrypt reads ciphertext from src, decrypts it with password, and writes
// plaintext to dst. The Argon2id parameters are read from the file header.
// Returns ErrWrongPassword if authentication fails.
func Decrypt(dst io.Writer, src io.Reader, password []byte) error {
	salt, params, err := readHeader(src)
	if err != nil {
		return err
	}

	key := deriveKey(password, salt, params)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("Failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("Failed to create GCM: %w", err)
	}

	var chunkIndex uint64
	sawFinal := false

	for {
		var flags [1]byte
		if _, err := io.ReadFull(src, flags[:]); err != nil {
			if errors.Is(err, io.EOF) {
				if sawFinal {
					break
				}
				return fmt.Errorf("Missing final encrypted chunk marker. Remedy: Check backup-part completeness and file readability.")
			}
			return fmt.Errorf("Failed to read chunk flags: %w. Remedy: Check backup-part completeness and file readability.", err)
		}
		if sawFinal {
			return fmt.Errorf("Unexpected encrypted chunk after final marker. Remedy: Use an unmodified backup created by this RestoreSafe version.")
		}
		if flags[0] != 0 && flags[0] != chunkFlagFinal {
			return fmt.Errorf("Invalid encrypted chunk flags: %d. Remedy: Use an unmodified backup created by this RestoreSafe version.", flags[0])
		}

		var length uint32
		if err := binary.Read(src, binary.BigEndian, &length); err != nil {
			return fmt.Errorf("Failed to read chunk length: %w. Remedy: Check backup-part completeness and file readability.", err)
		}
		if length > maxEncryptedChunkSize {
			return fmt.Errorf("Invalid encrypted chunk length: %d. Remedy: Use an unmodified backup created by this RestoreSafe version.", length)
		}

		encrypted := make([]byte, length)
		if _, err := io.ReadFull(src, encrypted); err != nil {
			return fmt.Errorf("Failed to read chunk data: %w. Remedy: Check backup-part completeness and file readability.", err)
		}

		nonce := chunkNonce(chunkIndex)
		plaintext, err := gcm.Open(nil, nonce, encrypted, chunkAAD(chunkIndex, flags[0]))
		if err != nil {
			return ErrWrongPassword
		}

		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("Failed to write decrypted data: %w", err)
		}

		sawFinal = flags[0] == chunkFlagFinal
		chunkIndex++
	}

	return nil
}

func writeEncryptedChunk(w io.Writer, gcm cipher.AEAD, index uint64, plaintext []byte, isFinal bool) error {
	var flags byte
	if isFinal {
		flags = chunkFlagFinal
	}

	nonce := chunkNonce(index)
	encrypted := gcm.Seal(nil, nonce, plaintext, chunkAAD(index, flags))

	if _, err := w.Write([]byte{flags}); err != nil {
		return fmt.Errorf("Failed to write chunk flags: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(encrypted))); err != nil {
		return fmt.Errorf("Failed to write chunk length: %w", err)
	}
	if _, err := w.Write(encrypted); err != nil {
		return fmt.Errorf("Failed to write chunk data: %w", err)
	}
	return nil
}

// writeHeader writes the v1 file header to w.
func writeHeader(w io.Writer, salt []byte, params Argon2Params) error {
	if _, err := io.WriteString(w, magic); err != nil {
		return fmt.Errorf("Failed to write magic: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(saltLen)); err != nil {
		return fmt.Errorf("Failed to write salt length: %w", err)
	}
	if _, err := w.Write(salt); err != nil {
		return fmt.Errorf("Failed to write salt: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(chunkSize)); err != nil {
		return fmt.Errorf("Failed to write chunk size: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, params.Time); err != nil {
		return fmt.Errorf("Failed to write Argon2 time: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, params.MemoryKB); err != nil {
		return fmt.Errorf("Failed to write Argon2 memory: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(params.Threads)); err != nil {
		return fmt.Errorf("Failed to write Argon2 threads: %w", err)
	}
	return nil
}

// readHeader reads and validates the v1 file header from r, returning the salt
// and Argon2id parameters stored in the header.
func readHeader(r io.Reader) ([]byte, Argon2Params, error) {
	magicBuf := make([]byte, len(magic))
	if _, err := io.ReadFull(r, magicBuf); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read magic: %w. Remedy: Check that the backup file is complete and readable.", err)
	}

	// Check the 6-byte identifier prefix before inspecting the version byte,
	// so that a version mismatch produces a clear message rather than a generic
	// "Invalid file format" error.
	if string(magicBuf[:len(magicPrefix)]) != magicPrefix {
		return nil, Argon2Params{}, fmt.Errorf("Invalid file format (not a RestoreSafe backup). Remedy: Select a valid RestoreSafe .enc backup file.")
	}
	fileVersion := magicBuf[len(magicPrefix)]
	if fileVersion != formatVersion {
		return nil, Argon2Params{}, fmt.Errorf(
			"Incompatible backup format %d (this RestoreSafe version uses backup format %d). Remedy: Restore with the RestoreSafe version that created this backup; it is recorded on the first line of the backup's .log file. Download other releases: https://github.com/phsc84/RestoreSafe/releases",
			fileVersion, formatVersion,
		)
	}

	var saltLength uint32
	if err := binary.Read(r, binary.BigEndian, &saltLength); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read salt length: %w. Remedy: Check that the backup file is complete and readable.", err)
	}
	if saltLength != saltLen {
		return nil, Argon2Params{}, fmt.Errorf("Invalid salt length: %d. Remedy: Use an unmodified backup created by this RestoreSafe version.", saltLength)
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read salt: %w. Remedy: Check that the backup file is complete and readable.", err)
	}

	// Read and validate stored chunk size for format compatibility.
	var storedChunkSize uint32
	if err := binary.Read(r, binary.BigEndian, &storedChunkSize); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read stored chunk size: %w. Remedy: Check that the backup file is complete and readable.", err)
	}
	if storedChunkSize != uint32(chunkSize) {
		return nil, Argon2Params{}, fmt.Errorf("Unsupported chunk size in backup header: %d. Remedy: Use a backup created by this RestoreSafe version.", storedChunkSize)
	}

	// Read Argon2id parameters stored at encryption time.
	var argonTime, argonMemoryKB, argonThreads uint32
	if err := binary.Read(r, binary.BigEndian, &argonTime); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read Argon2 time: %w. Remedy: Check that the backup file is complete and readable.", err)
	}
	if err := binary.Read(r, binary.BigEndian, &argonMemoryKB); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read Argon2 memory: %w. Remedy: Check that the backup file is complete and readable.", err)
	}
	if err := binary.Read(r, binary.BigEndian, &argonThreads); err != nil {
		return nil, Argon2Params{}, fmt.Errorf("Failed to read Argon2 threads: %w. Remedy: Check that the backup file is complete and readable.", err)
	}

	// Validate the stored parameters against the same bounds enforced for the
	// config before handing them to argon2.IDKey. Without this, a corrupted or
	// tampered header could request a multi-terabyte allocation (OOM) or a
	// parallelism of 0 (panic via the uint8 truncation below), so a single
	// bit-flip must produce a clean "corrupted header" error instead. argonThreads
	// is validated as a uint32 here, before the uint8 truncation, so a stored
	// value of 256 (which would truncate to 0) is still rejected.
	if err := validateArgon2Params(argonTime, argonMemoryKB, argonThreads, "in backup header", "Remedy: Use an unmodified backup created by this RestoreSafe version."); err != nil {
		return nil, Argon2Params{}, err
	}

	params := Argon2Params{
		Time:     argonTime,
		MemoryKB: argonMemoryKB,
		Threads:  uint8(argonThreads),
	}
	return salt, params, nil
}

// chunkNonce derives a deterministic 12-byte nonce from the chunk index
// (low 8 bytes = index, high 4 bytes = 0).
//
// A counter nonce is safe here because of the (key, nonce) uniqueness invariant:
// every call to Encrypt generates a fresh random salt and therefore a unique key
// for that stream, while chunks within a single stream are numbered by a
// strictly increasing counter. No (key, nonce) pair is ever reused across or
// within streams, which is the requirement AES-GCM relies on.
func chunkNonce(index uint64) []byte {
	nonce := make([]byte, nonceLen)
	binary.BigEndian.PutUint64(nonce[4:], index)
	return nonce
}

// chunkAAD builds the GCM associated data for a chunk: the 8-byte chunk index
// followed by the 1-byte flags field.
//
// The flags byte is the load-bearing part: it binds the final-chunk marker to
// authentication so that clearing it on the real last chunk (to hide a dropped
// tail) fails gcm.Open instead of silently decrypting. Truncation that removes
// whole trailing chunks is then caught by the sawFinal check in Decrypt.
//
// The index is defense-in-depth and is largely redundant with the counter
// nonce: chunkNonce already encodes the index, so reordering or swapping chunks
// changes the nonce and fails authentication regardless of the AAD. It is
// included anyway so chunk position is bound explicitly rather than implicitly.
func chunkAAD(index uint64, flags byte) []byte {
	aad := make([]byte, 9)
	binary.BigEndian.PutUint64(aad[:8], index)
	aad[8] = flags
	return aad
}
