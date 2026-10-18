//go:build windows

package yubikey

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ── YubiKey challenge (in the key slots of the backup header) ─────────────────

// ChallengeData is the YubiKey challenge of a key slot in the backup header.
// Version 1.
//
// Sum is a SHA-256 checksum over the other fields. It detects accidental
// corruption of the challenge and lets restore report a damaged challenge
// instead of a misleading "wrong password" failure when, for example, a
// bit-flip leaves Salt the right length but with the wrong bytes. It is NOT a defense against deliberate tampering
// (an attacker can recompute it): the cryptographic binding between a backup and
// its credential ID + salt is provided by Argon2id key derivation and AES-GCM
// authentication — any change to those values yields the wrong key and fails
// decryption.
type ChallengeData struct {
	Version    int    `json:"v"`
	NoPassword bool   `json:"nopw"`
	CredID     string `json:"cred_id"`       // base64 std-encoding of FIDO2 credential ID
	Salt       string `json:"salt"`          // base64 std-encoding of 32-byte hmac-secret salt
	Sum        string `json:"sum,omitempty"` // base64 std-encoding of SHA-256 over the fields above
}

// challengeChecksum returns the base64 SHA-256 checksum over the integrity-
// relevant challenge fields (everything except Sum itself).
func challengeChecksum(cd ChallengeData) string {
	canonical := fmt.Sprintf("v=%d;nopw=%t;cred_id=%s;salt=%s", cd.Version, cd.NoPassword, cd.CredID, cd.Salt)
	sum := sha256.Sum256([]byte(canonical))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// ValidateChallengeJSON reports whether s is a well-formed FIDO2 challenge JSON string.
func ValidateChallengeJSON(s string) error {
	_, err := parseChallengeData(s)
	return err
}

// ParseChallengeJSON parses and validates a FIDO2 challenge JSON string.
func ParseChallengeJSON(s string) (ChallengeData, error) {
	return parseChallengeData(s)
}

func parseChallengeData(s string) (ChallengeData, error) {
	if s == "" {
		return ChallengeData{}, fmt.Errorf("challenge data is empty")
	}
	var cd ChallengeData
	if err := json.Unmarshal([]byte(s), &cd); err != nil {
		return ChallengeData{}, fmt.Errorf("challenge data is not valid JSON: %w", err)
	}
	credID, err := base64.StdEncoding.DecodeString(cd.CredID)
	if err != nil || len(credID) == 0 || len(credID) > fido2CredIDMax {
		return ChallengeData{}, fmt.Errorf("challenge credential ID is missing or invalid")
	}
	salt, err := base64.StdEncoding.DecodeString(cd.Salt)
	if err != nil || len(salt) != fido2SaltSize {
		return ChallengeData{}, fmt.Errorf("challenge salt is missing or wrong length (want %d bytes)", fido2SaltSize)
	}
	// Verify the integrity checksum when present. Files written by this version
	// always include it; tolerate its absence so externally constructed v2
	// challenges remain valid.
	if cd.Sum != "" && cd.Sum != challengeChecksum(cd) {
		return ChallengeData{}, fmt.Errorf("YubiKey challenge is damaged (integrity checksum mismatch)")
	}
	return cd, nil
}
