package security

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Recovery codes are 25 random Crockford Base32 characters (125 bits)
// followed by a 5-character checksum group, displayed as 6 groups of 5:
// 7KQ2M-X9D4T-...-CHECK. The checksum catches typos before the slow Argon2
// derivation.
const (
	recoveryAlphabet   = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	recoveryDataChars  = 25
	recoveryCheckChars = 5
	recoveryGroupSize  = 5
)

// RecoveryCode is a normalized recovery code.
type RecoveryCode struct {
	data  string // 25 data characters
	check string // 5 checksum characters
}

// Secret returns the bytes used as the recovery slot's key material. The
// caller should zero them after use.
func (c RecoveryCode) Secret() []byte { return []byte(c.data) }

// Check returns the checksum group stored in the recovery slot.
func (c RecoveryCode) Check() string { return c.check }

// String returns the code in display form (6 groups of 5, dash-separated).
func (c RecoveryCode) String() string {
	all := c.data + c.check
	groups := make([]string, 0, len(all)/recoveryGroupSize)
	for i := 0; i < len(all); i += recoveryGroupSize {
		groups = append(groups, all[i:i+recoveryGroupSize])
	}
	return strings.Join(groups, "-")
}

// GenerateRecoveryCode creates a new random recovery code.
func GenerateRecoveryCode() (RecoveryCode, error) {
	raw, err := RandomBytes(16) // 128 random bits, 125 are used
	if err != nil {
		return RecoveryCode{}, err
	}
	defer ZeroBytes(raw)
	var b strings.Builder
	var acc uint64
	bits := 0
	for _, by := range raw {
		acc = acc<<8 | uint64(by)
		bits += 8
		for bits >= 5 && b.Len() < recoveryDataChars {
			bits -= 5
			b.WriteByte(recoveryAlphabet[(acc>>uint(bits))&0x1F])
		}
	}
	data := b.String()
	return RecoveryCode{data: data, check: recoveryChecksum(data)}, nil
}

// ParseRecoveryCode normalizes user input (case-insensitive; spaces and
// dashes ignored; I/L read as 1 and O as 0, as in Crockford Base32) and checks
// length and checksum.
func ParseRecoveryCode(input string) (RecoveryCode, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r == 'I' || r == 'L':
			r = '1'
		case r == 'O':
			r = '0'
		}
		if !strings.ContainsRune(recoveryAlphabet, r) {
			return RecoveryCode{}, fmt.Errorf("The recovery code contains the invalid character %q. Remedy: Check your recovery code note.", r)
		}
		b.WriteRune(r)
	}
	all := b.String()
	if len(all) != recoveryDataChars+recoveryCheckChars {
		return RecoveryCode{}, fmt.Errorf("The recovery code must have %d characters (6 groups of 5), got %d. Remedy: Check your recovery code note.", recoveryDataChars+recoveryCheckChars, len(all))
	}
	code := RecoveryCode{data: all[:recoveryDataChars], check: all[recoveryDataChars:]}
	if recoveryChecksum(code.data) != code.check {
		return RecoveryCode{}, fmt.Errorf("The recovery code contains a typo (checksum mismatch). Remedy: Check your recovery code note.")
	}
	return code, nil
}

// ValidRecoveryCheck reports whether s is a well-formed checksum group.
func ValidRecoveryCheck(s string) bool {
	if len(s) != recoveryCheckChars {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(recoveryAlphabet, r) {
			return false
		}
	}
	return true
}

func recoveryChecksum(data string) string {
	sum := sha256.Sum256([]byte("RestoreSafe recovery code|" + data))
	var b strings.Builder
	acc := uint64(sum[0])<<24 | uint64(sum[1])<<16 | uint64(sum[2])<<8 | uint64(sum[3])
	for i := 0; i < recoveryCheckChars; i++ {
		shift := uint(32 - 5*(i+1))
		b.WriteByte(recoveryAlphabet[(acc>>shift)&0x1F])
	}
	return b.String()
}
