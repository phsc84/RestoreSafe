package util

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

func TestSameVolume(t *testing.T) {
	t.Parallel()

	if !SameVolume(`M:\Backups`, `M:\Restore`) {
		t.Fatal("expected same mapped drive letter to be treated as same volume")
	}
	if SameVolume(`M:\Backups`, `N:\Restore`) {
		t.Fatal("expected different drive letters to be treated as different volumes")
	}
	if !SameVolume(`\\server\share\Backups`, `\\server\share\Restore`) {
		t.Fatal("expected same UNC share to be treated as same volume")
	}
	if SameVolume(`\\server\share-a\Backups`, `\\server\share-b\Restore`) {
		t.Fatal("expected different UNC shares to be treated as different volumes")
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

func TestVolumeDisplay(t *testing.T) {
	t.Parallel()

	if got := VolumeDisplay(`M:\Backups\Directory`); got != "M:" {
		t.Fatalf("expected drive display M:, got %q", got)
	}
	if got := VolumeDisplay(`\\server\share\Directory`); got != "//server/share" {
		t.Fatalf("expected UNC display //server/share, got %q", got)
	}
}
