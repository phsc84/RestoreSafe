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

// ReadPassword prints the prompt text to stdout and reads a password without echo.
func ReadPassword(prompt string) ([]byte, error) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	password, err := term.ReadPassword(fd)
	fmt.Println() // newline after hidden input
	if err != nil {
		return nil, fmt.Errorf("Failed to read password: %w. Remedy: Ensure stdin is available and retry.", err)
	}
	return password, nil
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
