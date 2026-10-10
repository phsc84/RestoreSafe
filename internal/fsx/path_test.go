package fsx

import (
	"path/filepath"
	"testing"
)

func TestResolveDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	relative := ResolveDir("sub/directory", base)
	expected := filepath.Join(base, "sub", "directory")
	if relative != expected {
		t.Fatalf("expected %q, got %q", expected, relative)
	}

	absolute := filepath.Join(base, "already-absolute")
	if got := ResolveDir(absolute, "ignored"); got != absolute {
		t.Fatalf("expected absolute path unchanged, got %q", got)
	}
}

func TestNormalizePathKey(t *testing.T) {
	t.Parallel()

	// Forward slashes are normalised to backslashes and the result is lowercased.
	if got := NormalizePathKey(`C:/Users/Foo/Documents`); got != `c:\users\foo\documents` {
		t.Fatalf("expected normalised lowercase backslash path, got %q", got)
	}
	// Redundant elements are cleaned away.
	if got := NormalizePathKey(`C:\Users\..\Users\Bar\.\Docs`); got != `c:\users\bar\docs` {
		t.Fatalf("expected cleaned path, got %q", got)
	}
	// Mixed-case variants of the same path produce an identical key.
	if NormalizePathKey(`M:\Backups`) != NormalizePathKey(`m:/BACKUPS`) {
		t.Fatal("expected case- and separator-insensitive keys to match")
	}
}
