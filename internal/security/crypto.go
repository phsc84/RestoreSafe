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
//   - Default parameters: 64 MB memory, 3 iterations, 4 threads
//   - Parameters are configurable and stored in the file header
//
// Stream chunking
//   - Large files are split into fixed-size chunks (chunkSize = 8 MB)
//   - Each chunk gets its own nonce derived deterministically from the
//     chunk index, preventing nonce reuse across chunks of the same stream
//   - A 4-byte big-endian length prefix is written before each encrypted chunk
//   - This avoids temp files and limits RAM usage to ~2× chunkSize
//
// File header layout (all values big-endian), format version 2:
//
//	[6]  magic prefix "RSBKP\x00"
//	[1]  format version (currently 2)
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
	formatVersion = byte(2)
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
)

// Argon2Params holds the Argon2id key-derivation parameters.
// All three values are stored in the file header so that decryption always
// uses the exact parameters that were in effect during encryption.
type Argon2Params struct {
	Time     uint32 // number of iterations (passes over memory)
	MemoryKB uint32 // working memory in kibibytes
	Threads  uint8  // degree of parallelism
}

// DefaultArgon2Params are the OWASP-recommended defaults:
// 64 MB of memory, 3 iterations, 4 parallel threads.
var DefaultArgon2Params = Argon2Params{
	Time:     3,
	MemoryKB: 64 * 1024,
	Threads:  4,
}

// magic is the full 8-byte header marker: prefix + version + reserved byte.
// Derived from magicPrefix + formatVersion so that bumping formatVersion
// automatically updates the on-disk marker without a manual string edit.
var magic = magicPrefix + string([]byte{formatVersion, 0x00})

// ErrWrongPassword is returned when decryption authentication fails.
// No trailing period — callers append ". Remedy: …" themselves.
var ErrWrongPassword = errors.New("Wrong password or corrupted file")

// deriveKey derives a 256-bit AES key from the password and salt using Argon2id.
func deriveKey(password, salt []byte, params Argon2Params) []byte {
	return argon2.IDKey(password, salt, params.Time, params.MemoryKB, params.Threads, keyLen)
}

// Encrypt reads plaintext from src, encrypts it with password and params, and writes
// ciphertext to dst. The function streams data in chunkSize chunks so that
// arbitrarily large files can be processed with constant memory.
func Encrypt(dst io.Writer, src io.Reader, password []byte, params Argon2Params) error {
	// Generate a random salt.
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("Failed to generate salt: %w. Remedy: Retry the operation and ensure the OS cryptographic provider is available.", err)
	}

	key := deriveKey(password, salt, params)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("Failed to create AES cipher: %w. Remedy: Verify the runtime environment and restart the application.", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("Failed to create GCM: %w. Remedy: Verify the runtime environment and restart the application.", err)
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
		if n == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("Failed to read plaintext: %w. Remedy: Check source-file readability and permissions.", readErr)
		}

		nonce := chunkNonce(chunkIndex)
		encrypted := gcm.Seal(nil, nonce, buf[:n], nil)

		// Write 4-byte length prefix + ciphertext.
		length := uint32(len(encrypted))
		if err := binary.Write(dst, binary.BigEndian, length); err != nil {
			return fmt.Errorf("Failed to write chunk length: %w. Remedy: Check destination write permissions and free disk space.", err)
		}
		if _, err := dst.Write(encrypted); err != nil {
			return fmt.Errorf("Failed to write chunk data: %w. Remedy: Check destination write permissions and free disk space.", err)
		}

		chunkIndex++
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
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
		return fmt.Errorf("Failed to create AES cipher: %w. Remedy: Verify the runtime environment and restart the application.", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("Failed to create GCM: %w. Remedy: Verify the runtime environment and restart the application.", err)
	}

	var chunkIndex uint64

	for {
		var length uint32
		if err := binary.Read(src, binary.BigEndian, &length); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
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
		plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
		if err != nil {
			return ErrWrongPassword
		}

		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("Failed to write decrypted data: %w. Remedy: Check destination write permissions and free disk space.", err)
		}

		chunkIndex++
	}

	return nil
}

// writeHeader writes the v2 file header to w.
func writeHeader(w io.Writer, salt []byte, params Argon2Params) error {
	if _, err := io.WriteString(w, magic); err != nil {
		return fmt.Errorf("Failed to write magic: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(saltLen)); err != nil {
		return fmt.Errorf("Failed to write salt length: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	if _, err := w.Write(salt); err != nil {
		return fmt.Errorf("Failed to write salt: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(chunkSize)); err != nil {
		return fmt.Errorf("Failed to write chunk size: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	if err := binary.Write(w, binary.BigEndian, params.Time); err != nil {
		return fmt.Errorf("Failed to write Argon2 time: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	if err := binary.Write(w, binary.BigEndian, params.MemoryKB); err != nil {
		return fmt.Errorf("Failed to write Argon2 memory: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(params.Threads)); err != nil {
		return fmt.Errorf("Failed to write Argon2 threads: %w. Remedy: Check destination write permissions and free disk space.", err)
	}
	return nil
}

// readHeader reads and validates the v2 file header from r, returning the salt
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
			"Incompatible backup format: version %d (this RestoreSafe version uses format %d). Remedy: Use the version of RestoreSafe that created this backup to restore it.",
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

	params := Argon2Params{
		Time:     argonTime,
		MemoryKB: argonMemoryKB,
		Threads:  uint8(argonThreads),
	}
	return salt, params, nil
}

// chunkNonce derives a deterministic 12-byte nonce from the chunk index.
// Using a counter nonce is safe because each chunk uses a freshly derived key
// per backup run (different salt → different key).
func chunkNonce(index uint64) []byte {
	nonce := make([]byte, nonceLen)
	binary.BigEndian.PutUint64(nonce[4:], index)
	return nonce
}
