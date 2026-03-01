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
//   - Parameters: 64 MB memory, 3 iterations, 4 threads
//
// Stream chunking
//   - Large files are split into fixed-size chunks (chunkSize = 8 MB)
//   - Each chunk gets its own nonce derived deterministically from the
//     chunk index, preventing nonce reuse across chunks of the same stream
//   - A 4-byte big-endian length prefix is written before each encrypted chunk
//   - This avoids temp files and limits RAM usage to ~2× chunkSize
//
// File header layout (all values big-endian):
//
//	[8]  magic  "RSBKP\x00\x01\x00"
//	[4]  salt length (always saltLen = 32)
//	[32] salt
//	[4]  chunk size (bytes, currently 8388608)
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
	// magic identifies a RestoreSafe encrypted file.
	magic = "RSBKP\x00\x01\x00"
	// saltLen is the byte length of the Argon2id salt.
	saltLen = 32
	// keyLen is the AES-256 key length in bytes.
	keyLen = 32
	// nonceLen is the GCM nonce length.
	nonceLen = 12
	// chunkSize is the plaintext chunk size for streaming.
	chunkSize = 8 * 1024 * 1024 // 8 MB
)

// Argon2id parameters (OWASP recommended minimum for high-security use).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4
)

// ErrWrongPassword is returned when decryption authentication fails.
var ErrWrongPassword = errors.New("Wrong password or corrupted file.")

// deriveKey derives a 256-bit AES key from the password and salt using Argon2id.
func deriveKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyLen)
}

// Encrypt reads plaintext from src, encrypts it with password, and writes
// ciphertext to dst. The function streams data in chunkSize chunks so that
// arbitrarily large files can be processed with constant memory.
func Encrypt(dst io.Writer, src io.Reader, password []byte) error {
	// Generate a random salt.
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("Failed to generate salt: %w", err)
	}

	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("Failed to create AES-Cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("Failed to create GCM: %w", err)
	}

	// Write file header.
	if err := writeHeader(dst, salt); err != nil {
		return err
	}

	// Stream plaintext in chunks.
	buf := make([]byte, chunkSize)
	var chunkIndex uint64

	for {
		n, readErr := io.ReadFull(src, buf)
		if n == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return fmt.Errorf("Failed to read plaintext: %w", readErr)
		}

		nonce := chunkNonce(chunkIndex)
		encrypted := gcm.Seal(nil, nonce, buf[:n], nil)

		// Write 4-byte length prefix + ciphertext.
		length := uint32(len(encrypted))
		if err := binary.Write(dst, binary.BigEndian, length); err != nil {
			return fmt.Errorf("Failed to write chunk length: %w", err)
		}
		if _, err := dst.Write(encrypted); err != nil {
			return fmt.Errorf("Failed to write chunk data: %w", err)
		}

		chunkIndex++
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	return nil
}

// Decrypt reads ciphertext from src, decrypts it with password, and writes
// plaintext to dst. Returns ErrWrongPassword if authentication fails.
func Decrypt(dst io.Writer, src io.Reader, password []byte) error {
	salt, err := readHeader(src)
	if err != nil {
		return err
	}

	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("Failed to create AES-Cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("Failed to create GCM: %w", err)
	}

	var chunkIndex uint64

	for {
		var length uint32
		if err := binary.Read(src, binary.BigEndian, &length); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("Failed to read chunk length: %w", err)
		}

		encrypted := make([]byte, length)
		if _, err := io.ReadFull(src, encrypted); err != nil {
			return fmt.Errorf("Failed to read chunk data: %w", err)
		}

		nonce := chunkNonce(chunkIndex)
		plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
		if err != nil {
			return ErrWrongPassword
		}

		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("Failed to write decrypted data: %w", err)
		}

		chunkIndex++
	}

	return nil
}

// writeHeader writes the fixed-size file header to w.
func writeHeader(w io.Writer, salt []byte) error {
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
	return nil
}

// readHeader reads and validates the file header from r, returning the salt.
func readHeader(r io.Reader) ([]byte, error) {
	magicBuf := make([]byte, len(magic))
	if _, err := io.ReadFull(r, magicBuf); err != nil {
		return nil, fmt.Errorf("Failed to read magic: %w", err)
	}
	if string(magicBuf) != magic {
		return nil, fmt.Errorf("Invalid file format (not a RestoreSafe backup).")
	}

	var saltLength uint32
	if err := binary.Read(r, binary.BigEndian, &saltLength); err != nil {
		return nil, fmt.Errorf("Failed to read salt length: %w", err)
	}
	if saltLength != saltLen {
		return nil, fmt.Errorf("Invalid salt length: %d", saltLength)
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, fmt.Errorf("Failed to read salt: %w", err)
	}

	// Read (and ignore) stored chunk size — we use the embedded value.
	var storedChunkSize uint32
	if err := binary.Read(r, binary.BigEndian, &storedChunkSize); err != nil {
		return nil, fmt.Errorf("Failed to read stored chunk size: %w", err)
	}

	return salt, nil
}

// chunkNonce derives a deterministic 12-byte nonce from the chunk index.
// Using a counter nonce is safe because each chunk uses a freshly derived key
// per backup run (different salt → different key).
func chunkNonce(index uint64) []byte {
	nonce := make([]byte, nonceLen)
	binary.BigEndian.PutUint64(nonce[4:], index)
	return nonce
}
