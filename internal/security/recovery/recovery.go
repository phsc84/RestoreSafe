// Package recovery creates and checks recovery codes: 25 random characters
// plus a 5-character checksum, shown as 6 groups of 5. A recovery code unlocks
// a key set without password or YubiKey.
package recovery

import (
	"RestoreSafe/internal/problem"
	"RestoreSafe/internal/security/cryptox"
	"bytes"
	"crypto/sha256"
	"strings"
	"unicode/utf8"
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

// Code is a normalized recovery code. Its data characters are kept as bytes,
// never as a string, so that Zero can clear them; the checksum is not secret,
// as the recovery slot stores it in plain.
type Code struct {
	data  []byte // 25 data characters
	check string // 5 checksum characters
}

// Secret returns a copy of the bytes used as the recovery slot's key
// material. The caller should zero them after use.
func (c Code) Secret() []byte { return bytes.Clone(c.data) }

// Check returns the checksum group stored in the recovery slot.
func (c Code) Check() string { return c.check }

// Display returns the code in display form (6 groups of 5, dash-separated).
// The caller should zero it after use.
func (c Code) Display() []byte {
	groups := (recoveryDataChars + recoveryCheckChars) / recoveryGroupSize
	out := make([]byte, 0, groups*(recoveryGroupSize+1)-1)
	for i := range recoveryDataChars + recoveryCheckChars {
		if i > 0 && i%recoveryGroupSize == 0 {
			out = append(out, '-')
		}
		if i < recoveryDataChars {
			out = append(out, c.data[i])
		} else {
			out = append(out, c.check[i-recoveryDataChars])
		}
	}
	return out
}

// Zero clears the code's data characters.
func (c Code) Zero() { cryptox.ZeroBytes(c.data) }

// Generate creates a new random recovery code.
func Generate() (Code, error) {
	raw, err := cryptox.RandomBytes(16) // 128 random bits, 125 are used
	if err != nil {
		return Code{}, err
	}
	defer cryptox.ZeroBytes(raw)
	data := make([]byte, 0, recoveryDataChars)
	var acc uint64
	bits := 0
	for _, by := range raw {
		acc = acc<<8 | uint64(by)
		bits += 8
		for bits >= 5 && len(data) < recoveryDataChars {
			bits -= 5
			data = append(data, recoveryAlphabet[(acc>>uint(bits))&0x1F])
		}
	}
	return Code{data: data, check: recoveryChecksum(data)}, nil
}

// Parse normalizes user input (case-insensitive; spaces and
// dashes ignored; I/L read as 1 and O as 0, as in Crockford Base32) and checks
// length and checksum. It does not keep input; the caller zeroes it.
func Parse(input []byte) (Code, error) {
	all := make([]byte, 0, recoveryDataChars+recoveryCheckChars)
	n := 0
	fail := func(err error) (Code, error) {
		cryptox.ZeroBytes(all[:cap(all)])
		return Code{}, err
	}
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRune(input[i:])
		i += size
		if 'a' <= r && r <= 'z' {
			r -= 'a' - 'A'
		}
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r == 'I' || r == 'L':
			r = '1'
		case r == 'O':
			r = '0'
		}
		if r >= utf8.RuneSelf || !strings.ContainsRune(recoveryAlphabet, r) {
			return fail(problem.Errorf("The recovery code contains the invalid character %q.", r).WithRemedy("Check your recovery code note."))
		}
		n++
		if len(all) < cap(all) { // never grows, so no copy is left behind
			all = append(all, byte(r))
		}
	}
	if n != recoveryDataChars+recoveryCheckChars {
		return fail(problem.Errorf("The recovery code must have %d characters (6 groups of 5), got %d.", recoveryDataChars+recoveryCheckChars, n).WithRemedy("Check your recovery code note."))
	}
	code := Code{data: all[:recoveryDataChars], check: string(all[recoveryDataChars:])}
	if recoveryChecksum(code.data) != code.check {
		return fail(problem.New("The recovery code contains a typo (checksum mismatch).").WithRemedy("Check your recovery code note."))
	}
	return code, nil
}

// ValidCheck reports whether s is a well-formed checksum group.
func ValidCheck(s string) bool {
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

func recoveryChecksum(data []byte) string {
	h := sha256.New()
	h.Write([]byte("RestoreSafe recovery code|"))
	h.Write(data)
	sum := h.Sum(nil)
	acc := uint64(sum[0])<<24 | uint64(sum[1])<<16 | uint64(sum[2])<<8 | uint64(sum[3])
	check := make([]byte, recoveryCheckChars)
	for i := range check {
		shift := uint(32 - 5*(i+1))
		check[i] = recoveryAlphabet[(acc>>shift)&0x1F]
	}
	return string(check)
}
