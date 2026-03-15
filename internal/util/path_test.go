package util

import (
	"path/filepath"
	"testing"
)

func TestResolveDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	relative := ResolveDir("sub/folder", base)
	expected := filepath.Join(base, "sub", "folder")
	if relative != expected {
		t.Fatalf("expected %q, got %q", expected, relative)
	}

	absolute := filepath.Join(base, "already-absolute")
	if got := ResolveDir(absolute, "ignored"); got != absolute {
		t.Fatalf("expected absolute path unchanged, got %q", got)
	}
}

func TestFormatBytesBinary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.00 KiB"},
		{1536, "1.50 KiB"},
		{1024 * 1024, "1.00 MiB"},
		{3 * 1024 * 1024 * 1024, "3.00 GiB"},
	}

	for _, tc := range cases {
		if got := FormatBytesBinary(tc.in); got != tc.want {
			t.Fatalf("FormatBytesBinary(%d): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}
