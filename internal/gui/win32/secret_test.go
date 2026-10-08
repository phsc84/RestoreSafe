package win32

import (
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSecretUTF16MatchesStringConversion(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"", "K7QF2-9M2DX", "Grüße €", "key 😀"} {
		want, err := windows.UTF16FromString(s)
		if err != nil {
			t.Fatal(err)
		}
		got := secretUTF16([]byte(s))
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", s, got, want)
		}
		if cap(got) != len(s)+1 {
			t.Errorf("%q: capacity %d, want %d: the buffer grew and left a copy", s, cap(got), len(s)+1)
		}
	}
}
