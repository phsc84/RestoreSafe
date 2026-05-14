// Package prompt provides secure password input helpers.
package security

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPassword prints the prompt text to stdout and reads a password,
// displaying '*' for each character typed.
func ReadPassword(prompt string) ([]byte, error) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("Failed to read password: %w. Remedy: Ensure stdin is available and retry.", err)
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			fmt.Print("\r\n")
			return nil, fmt.Errorf("Failed to read password: %w. Remedy: Ensure stdin is available and retry.", err)
		}
		switch b[0] {
		case '\r', '\n':
			fmt.Print("\r\n")
			return buf, nil
		case 0x7f, 0x08: // backspace / delete
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Print("\b \b")
			}
		case 0x03: // Ctrl+C
			fmt.Print("\r\n")
			return nil, fmt.Errorf("Password input cancelled.")
		case 0x04: // Ctrl+D (EOF)
			fmt.Print("\r\n")
			return nil, fmt.Errorf("Failed to read password: EOF. Remedy: Ensure stdin is available and retry.")
		default:
			if b[0] >= 0x20 { // printable ASCII
				buf = append(buf, b[0])
				fmt.Print("*")
			}
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
