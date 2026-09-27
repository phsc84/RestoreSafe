package interact

import (
	"errors"
	"strings"
	"testing"
)

// answers returns a password reader that returns inputs in order.
func answers(inputs ...string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		if len(inputs) == 0 {
			return nil, errors.New("Failed to read password: EOF")
		}
		in := inputs[0]
		inputs = inputs[1:]
		return []byte(in), nil
	}
}

func TestReadPasswordConfirmed(t *testing.T) {
	t.Parallel()

	if pw, err := ReadPasswordConfirmed(answers("secret", "secret"), "Password: ", "Confirm: "); err != nil || string(pw) != "secret" {
		t.Fatalf("matching passwords: got %q, %v", pw, err)
	}
	if _, err := ReadPasswordConfirmed(answers("secret", "other"), "Password: ", "Confirm: "); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("mismatch: expected ErrPasswordMismatch, got %v", err)
	}
	if _, err := ReadPasswordConfirmed(answers(""), "Password: ", "Confirm: "); !errors.Is(err, ErrPasswordEmpty) {
		t.Fatalf("empty: expected ErrPasswordEmpty, got %v", err)
	}
	for _, in := range [][]string{nil, {"secret"}} {
		_, err := ReadPasswordConfirmed(answers(in...), "Password: ", "Confirm: ")
		if err == nil || !strings.Contains(err.Error(), "Failed to read password") {
			t.Fatalf("read error after %d answer(s): got %v", len(in), err)
		}
	}
}
