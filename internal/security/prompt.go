// Package prompt provides secure password input helpers.
package security

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

// ZeroBytes overwrites b with zeros to reduce the window in which a password
// is present in process memory. Call it as soon as the password is no longer
// needed; use defer for correctness on all return paths.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ReadPassword prints the prompt text to stdout and reads a password,
// displaying '*' for each accepted character.
//
// UTF-8 multi-byte characters are accepted: bytes are buffered until a
// complete codepoint is assembled, then accepted if printable. Backspace
// removes the last complete codepoint (not just one byte). Non-printable or
// invalid bytes produce a terminal bell and are silently discarded.
func ReadPassword(prompt string) ([]byte, error) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("Failed to read password: %w. Remedy: Ensure stdin is available and retry.", err)
	}
	defer term.Restore(fd, oldState)

	var buf []byte     // accepted password bytes (complete codepoints only)
	var partial []byte // bytes of an incomplete multi-byte UTF-8 codepoint
	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			fmt.Print("\r\n")
			return nil, fmt.Errorf("Failed to read password: %w. Remedy: Ensure stdin is available and retry.", err)
		}
		switch b[0] {
		case '\r', '\n':
			partial = partial[:0]
			fmt.Print("\r\n")
			return buf, nil
		case 0x7f, 0x08: // backspace / delete
			partial = partial[:0]
			if len(buf) > 0 {
				_, runeSize := utf8.DecodeLastRune(buf)
				buf = buf[:len(buf)-runeSize]
				fmt.Print("\b \b")
			}
		case 0x03: // Ctrl+C
			fmt.Print("\r\n")
			return nil, fmt.Errorf("Password input cancelled.")
		case 0x04: // Ctrl+D (EOF)
			fmt.Print("\r\n")
			return nil, fmt.Errorf("Failed to read password: EOF. Remedy: Ensure stdin is available and retry.")
		default:
			if b[0] < 0x20 {
				// Non-printable control byte — alert and discard.
				fmt.Print("\a")
				continue
			}
			// Accumulate for UTF-8 decoding.
			partial = append(partial, b[0])

			if b[0] < 0x80 {
				// Single-byte ASCII codepoint (0x20–0x7E): process immediately.
				r := rune(partial[0])
				partial = partial[:0]
				if !unicode.IsPrint(r) {
					fmt.Print("\a")
					continue
				}
				buf = append(buf, byte(r))
				fmt.Print("*")
				continue
			}

			// High byte: may be part of a multi-byte UTF-8 codepoint.
			if !utf8.FullRune(partial) {
				// Sequence not yet complete — wait for more bytes,
				// but guard against impossibly long sequences.
				if len(partial) > utf8.UTFMax {
					partial = partial[:0]
					fmt.Print("\a")
				}
				continue
			}

			// Full codepoint (or provably invalid byte sequence) available.
			r, size := utf8.DecodeRune(partial)
			partial = partial[:0]
			if r == utf8.RuneError && size == 1 {
				// Invalid UTF-8 byte sequence — alert and discard.
				fmt.Print("\a")
				continue
			}
			if !unicode.IsPrint(r) {
				fmt.Print("\a")
				continue
			}
			buf = append(buf, []byte(string(r))...)
			fmt.Print("*")
		}
	}
}

// ReadPasswordConfirmedWithPrompts asks the user to enter and confirm a
// password using custom prompt texts.
func ReadPasswordConfirmedWithPrompts(firstPrompt, confirmPrompt string) ([]byte, error) {
	pw1, err := ReadPassword(firstPrompt)
	if err != nil {
		return nil, err
	}
	if len(pw1) == 0 {
		return nil, fmt.Errorf("Password must not be empty. Remedy: Enter a password with at least one character.")
	}

	pw2, err := ReadPassword(confirmPrompt)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(pw1, pw2) {
		return nil, fmt.Errorf("Passwords do not match. Remedy: Enter exactly the same password in the second prompt.")
	}

	return pw1, nil
}

// ReadLine reads a single line from stdin (with echo).
func ReadLine(promptText string) (string, error) {
	fmt.Print(promptText)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("Failed to read input: %w. Remedy: Ensure stdin is available and retry.", err)
	}
	return strings.TrimSpace(line), nil
}
