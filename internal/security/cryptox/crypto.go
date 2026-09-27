// Package cryptox provides the cryptographic primitives of container format 2:
// Argon2id key derivation, key wrapping, HKDF subkeys, and authenticated
// streaming encryption.
//
// # Design decisions
//
// Algorithm: AES-256-GCM
//   - Industry standard authenticated encryption (AEAD)
//   - Provides both confidentiality AND integrity/authenticity
//   - Detects tampering or wrong keys at decryption time
//
// KDF: Argon2id (RFC 9106)
//   - Winner of the Password Hashing Competition (2015)
//   - Memory-hard → resistant to GPU/ASIC brute-force
//   - Used to derive a key-encryption key (KEK) per key slot; the KEK wraps the
//     random master key of a key set (see package container)
//   - Default parameters: 512 MB memory, 3 iterations, 4 threads
//
// Subkeys: HKDF-SHA256
//   - The data and manifest section keys of every backup set are derived from
//     the key set master key, salted with the set's header hash, so every set
//     and every section has its own key
//
// Stream chunking
//   - Section plaintext is split into fixed-size chunks (ChunkSize = 8 MB)
//   - Each chunk gets its own nonce derived deterministically from the chunk
//     index, preventing nonce reuse within a stream
//   - A 1-byte flag field and 4-byte big-endian length prefix are written before
//     each encrypted chunk
//   - The caller-supplied AAD prefix, the chunk index, and the flags are GCM
//     associated data, binding the chunk to its set header and section as well
//     as chunk order and the final-chunk marker to authentication
//   - This avoids temp files and limits RAM usage to ~2× ChunkSize
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	// SaltLen is the byte length of Argon2id salts.
	SaltLen = 32
	// KeyLen is the AES-256 key length in bytes.
	KeyLen = 32
	// NonceLen is the GCM nonce length.
	NonceLen = 12
	// ChunkSize is the plaintext chunk size for streaming.
	ChunkSize = 8 * 1024 * 1024 // 8 MB
	// chunkPrefixLen is the per-chunk framing: 1 flag byte + 4 length bytes.
	chunkPrefixLen = 5
	// gcmTagLen is the GCM authentication tag length.
	gcmTagLen = 16
	// maxEncryptedChunkSize is the largest valid GCM-sealed chunk payload.
	maxEncryptedChunkSize = ChunkSize + gcmTagLen
	// FullEncryptedChunkSize is the on-disk size of every chunk except the last
	// one of a stream (framing + full ciphertext + tag). Fixed chunk sizes make
	// chunk positions computable for random access.
	FullEncryptedChunkSize = chunkPrefixLen + maxEncryptedChunkSize
	// chunkFlagFinal marks the final authenticated chunk in a stream.
	chunkFlagFinal = byte(1)
)

// Argon2id parameter bounds. This package owns the canonical bounds because it
// owns the key-derivation function (argon2.IDKey); package config derives its
// MB-based config bounds from these. Memory is expressed here in KiB to match
// the on-disk format and the KDF native units.
//
// ValidateArgon2Params enforces these on every derivation so that hostile or
// corrupted header values cannot trigger an OOM (a multi-terabyte memory
// request) or a panic (parallelism 0 via the uint8 truncation). The threads
// maximum also keeps the value within the uint8 range argon2 requires.
const (
	MinArgonTime     = 2
	MaxArgonTime     = 20
	MinArgonMemoryKB = 64 * 1024   // 64 MiB
	MaxArgonMemoryKB = 4096 * 1024 // 4096 MiB
	MinArgonThreads  = 1
	MaxArgonThreads  = 255
)

// Argon2Params holds the Argon2id key-derivation parameters.
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

// ErrWrongPassword is returned when a key slot cannot be opened: the password,
// YubiKey response, or recovery code does not match.
var ErrWrongPassword = errors.New("Wrong password or YubiKey")

// ErrCorrupted is returned when an encrypted chunk fails authentication after
// the key was already verified, i.e. the backup data was modified or damaged.
var ErrCorrupted = errors.New("Backup data failed authentication (corrupted or modified)")

// ValidateArgon2Params reports whether the Argon2id parameters fall within the
// enforced bounds. Values are taken as uint32 so raw header fields can be
// validated before the uint8 truncation of Threads: a stored thread count of
// 256 truncates to 0, so validating the post-truncation value would mask it.
// context and remedy are interpolated so the message reads naturally for both
// the header and the configuration path.
func ValidateArgon2Params(time, memoryKB, threads uint32, context, remedy string) error {
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

// RandomBytes returns n cryptographically random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("Failed to generate random bytes: %w", err)
	}
	return b, nil
}

// DeriveKEK derives a 256-bit key-encryption key from secret and salt with
// Argon2id. The parameters are validated first so invalid values can never
// reach argon2.IDKey.
func DeriveKEK(secret, salt []byte, params Argon2Params) ([]byte, error) {
	if len(salt) != SaltLen {
		return nil, fmt.Errorf("Invalid Argon2 salt length: %d (want %d).", len(salt), SaltLen)
	}
	if err := ValidateArgon2Params(params.Time, params.MemoryKB, uint32(params.Threads), "in key derivation parameters", "Remedy: Adjust the argon2 settings in config.yaml."); err != nil {
		return nil, err
	}
	return argon2.IDKey(secret, salt, params.Time, params.MemoryKB, params.Threads, KeyLen), nil
}

// SealKey encrypts plaintext (a key) with kek under nonce and aad.
func SealKey(kek, nonce, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceLen {
		return nil, fmt.Errorf("Invalid nonce length: %d (want %d).", len(nonce), NonceLen)
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

// OpenKey decrypts a key sealed with SealKey. It returns ErrWrongPassword when
// authentication fails, which is the expected outcome for a wrong credential.
func OpenKey(kek, nonce, ciphertext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceLen {
		return nil, fmt.Errorf("Invalid nonce length: %d (want %d).", len(nonce), NonceLen)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plaintext, nil
}

// DeriveSubkey derives a 256-bit subkey from master with HKDF-SHA256.
func DeriveSubkey(master, salt []byte, info string) ([]byte, error) {
	if len(master) != KeyLen {
		return nil, fmt.Errorf("Invalid master key length: %d (want %d).", len(master), KeyLen)
	}
	key, err := hkdf.Key(sha256.New, master, salt, info, KeyLen)
	if err != nil {
		return nil, fmt.Errorf("Failed to derive subkey: %w", err)
	}
	return key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("Invalid key length: %d (want %d).", len(key), KeyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("Failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("Failed to create GCM: %w", err)
	}
	return gcm, nil
}

// EncryptStream reads plaintext from src, encrypts it with key, and writes the
// chunk stream to dst. aadPrefix is prepended to every chunk's associated data.
// The function streams data in ChunkSize chunks so that arbitrarily large
// inputs are processed with constant memory. An empty input produces a single
// empty final chunk.
//
// The key must be unique per stream: the chunk nonce is a counter starting at
// 0, so reusing a key for a second stream would reuse (key, nonce) pairs.
// Package container guarantees this by deriving a fresh subkey per set and
// section.
func EncryptStream(dst io.Writer, src io.Reader, key, aadPrefix []byte) error {
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}

	buf := make([]byte, ChunkSize)
	var chunkIndex uint64
	for {
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("Failed to read plaintext: %w", readErr)
		}

		isFinal := errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF)
		if err := writeEncryptedChunk(dst, gcm, aadPrefix, chunkIndex, buf[:n], isFinal); err != nil {
			return err
		}
		if isFinal {
			return nil
		}
		chunkIndex++
	}
}

// DecryptStream reads a chunk stream written by EncryptStream from src,
// decrypts it with key, and writes plaintext to dst. src must end exactly after
// the final chunk; trailing bytes are rejected. Authentication failures return
// ErrCorrupted.
func DecryptStream(dst io.Writer, src io.Reader, key, aadPrefix []byte) error {
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}

	var chunkIndex uint64
	sawFinal := false
	encrypted := make([]byte, 0, maxEncryptedChunkSize)

	for {
		var prefix [chunkPrefixLen]byte
		if _, err := io.ReadFull(src, prefix[:1]); err != nil {
			if errors.Is(err, io.EOF) {
				if sawFinal {
					return nil
				}
				return fmt.Errorf("Missing final encrypted chunk marker. Remedy: Check backup-part completeness and file readability.")
			}
			return fmt.Errorf("Failed to read chunk flags: %w. Remedy: Check backup-part completeness and file readability.", err)
		}
		if sawFinal {
			return fmt.Errorf("Unexpected data after final encrypted chunk. Remedy: Use an unmodified backup created by RestoreSafe.")
		}
		flags := prefix[0]
		if flags != 0 && flags != chunkFlagFinal {
			return fmt.Errorf("Invalid encrypted chunk flags: %d. Remedy: Use an unmodified backup created by RestoreSafe.", flags)
		}

		if _, err := io.ReadFull(src, prefix[1:]); err != nil {
			return fmt.Errorf("Failed to read chunk length: %w. Remedy: Check backup-part completeness and file readability.", err)
		}
		length := binary.BigEndian.Uint32(prefix[1:])
		if length < gcmTagLen || length > maxEncryptedChunkSize {
			return fmt.Errorf("Invalid encrypted chunk length: %d. Remedy: Use an unmodified backup created by RestoreSafe.", length)
		}

		encrypted = encrypted[:length]
		if _, err := io.ReadFull(src, encrypted); err != nil {
			return fmt.Errorf("Failed to read chunk data: %w. Remedy: Check backup-part completeness and file readability.", err)
		}

		plaintext, err := gcm.Open(encrypted[:0], chunkNonce(chunkIndex), encrypted, chunkAAD(aadPrefix, chunkIndex, flags))
		if err != nil {
			return ErrCorrupted
		}
		if flags != chunkFlagFinal && len(plaintext) != ChunkSize {
			return fmt.Errorf("Invalid non-final chunk size: %d. Remedy: Use an unmodified backup created by RestoreSafe.", len(plaintext))
		}

		if len(plaintext) > 0 {
			if _, err := dst.Write(plaintext); err != nil {
				return fmt.Errorf("Failed to write decrypted data: %w", err)
			}
		}

		sawFinal = flags == chunkFlagFinal
		chunkIndex++
	}
}

func writeEncryptedChunk(w io.Writer, gcm cipher.AEAD, aadPrefix []byte, index uint64, plaintext []byte, isFinal bool) error {
	var flags byte
	if isFinal {
		flags = chunkFlagFinal
	}

	encrypted := gcm.Seal(nil, chunkNonce(index), plaintext, chunkAAD(aadPrefix, index, flags))

	var prefix [chunkPrefixLen]byte
	prefix[0] = flags
	binary.BigEndian.PutUint32(prefix[1:], uint32(len(encrypted)))
	if _, err := w.Write(prefix[:]); err != nil {
		return fmt.Errorf("Failed to write chunk header: %w", err)
	}
	if _, err := w.Write(encrypted); err != nil {
		return fmt.Errorf("Failed to write chunk data: %w", err)
	}
	return nil
}

// chunkNonce derives a deterministic 12-byte nonce from the chunk index
// (low 8 bytes = index, high 4 bytes = 0).
//
// A counter nonce is safe because every stream is encrypted with its own key
// (see EncryptStream), while chunks within a stream are numbered by a strictly
// increasing counter. No (key, nonce) pair is ever reused.
func chunkNonce(index uint64) []byte {
	nonce := make([]byte, NonceLen)
	binary.BigEndian.PutUint64(nonce[4:], index)
	return nonce
}

// chunkAAD builds the GCM associated data for a chunk: the caller's prefix,
// the 8-byte chunk index, and the 1-byte flags field.
//
// The prefix binds each chunk to its set header and section (package
// container passes header hash + section ID). The flags byte binds the
// final-chunk marker to authentication so that clearing it on the real last
// chunk (to hide a dropped tail) fails gcm.Open; truncation that removes whole
// trailing chunks is caught by the sawFinal check in DecryptStream. The index
// is defense-in-depth; the counter nonce already encodes it.
func chunkAAD(prefix []byte, index uint64, flags byte) []byte {
	aad := make([]byte, len(prefix)+9)
	copy(aad, prefix)
	binary.BigEndian.PutUint64(aad[len(prefix):], index)
	aad[len(prefix)+8] = flags
	return aad
}
