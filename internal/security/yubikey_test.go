package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveYkmanExecutableWithPrefersLookPath(t *testing.T) {
	lookPath := func(_ string) (string, error) {
		return "C:/custom/ykman.exe", nil
	}
	stat := func(_ string) (os.FileInfo, error) {
		t.Fatal("stat should not be called when LookPath succeeds")
		return nil, nil
	}
	getenv := func(_ string) string { return "" }

	resolved, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if resolved != "C:/custom/ykman.exe" {
		t.Fatalf("expected LookPath result, got: %q", resolved)
	}
}

func TestResolveYkmanExecutableWithFallsBackToWindowsInstallDir(t *testing.T) {
	base := `C:\Program Files`
	expected := filepath.Join(base, "Yubico", "YubiKey Manager", "ykman.exe")

	lookPath := func(_ string) (string, error) {
		return "", errors.New("not on PATH")
	}
	stat := func(path string) (os.FileInfo, error) {
		if path == expected {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	getenv := func(key string) string {
		switch key {
		case "ProgramFiles":
			return base
		case "ProgramW6432":
			return base
		case "ProgramFiles(x86)":
			return `C:\Program Files (x86)`
		default:
			return ""
		}
	}

	resolved, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if resolved != expected {
		t.Fatalf("expected fallback path %q, got: %q", expected, resolved)
	}
}

func TestResolveYkmanExecutableWithReturnsNotFound(t *testing.T) {
	lookPath := func(_ string) (string, error) {
		return "", errors.New("not found")
	}
	stat := func(_ string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	getenv := func(_ string) string { return "" }

	_, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows")
	if !errors.Is(err, ErrYubikeyNotFound) {
		t.Fatalf("expected ErrYubikeyNotFound, got: %v", err)
	}
}

func TestYkmanWindowsCandidatesWithBuildsExpectedPaths(t *testing.T) {
	base := `C:\Program Files`
	getenv := func(key string) string {
		switch key {
		case "ProgramFiles":
			return base
		case "ProgramW6432":
			return base
		case "ProgramFiles(x86)":
			return `C:\Program Files (x86)`
		default:
			return ""
		}
	}

	candidates := ykmanWindowsCandidatesWith(getenv)
	if len(candidates) != 4 {
		t.Fatalf("expected 4 candidates (2 unique bases), got %d", len(candidates))
	}

	expectedFirst := filepath.Join(base, "Yubico", "YubiKey Manager", "ykman.exe")
	if candidates[0] != expectedFirst {
		t.Fatalf("expected first candidate %q, got %q", expectedFirst, candidates[0])
	}
}
