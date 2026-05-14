package security

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	resolved, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows", "")
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

	resolved, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows", "")
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

	_, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows", "")
	if !errors.Is(err, ErrYubikeyNotFound) {
		t.Fatalf("expected ErrYubikeyNotFound, got: %v", err)
	}
}

func TestResolveYkmanExecutableWithFindsInExeDir(t *testing.T) {
	exeDir := `C:\RestoreSafe`
	expected := filepath.Join(exeDir, "ykman.exe")

	lookPath := func(_ string) (string, error) {
		return "", errors.New("not on PATH")
	}
	stat := func(path string) (os.FileInfo, error) {
		if path == expected {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	getenv := func(_ string) string { return "" }

	resolved, err := resolveYkmanExecutableWith(lookPath, stat, getenv, "windows", exeDir)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if resolved != expected {
		t.Fatalf("expected exeDir path %q, got: %q", expected, resolved)
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

func TestCheckYubiKeyConnectedSuccess(t *testing.T) {
	prevResolve := resolveYkmanExecutableFn
	prevList := ykmanListOutput
	t.Cleanup(func() {
		resolveYkmanExecutableFn = prevResolve
		ykmanListOutput = prevList
	})

	resolveYkmanExecutableFn = func() (string, error) {
		return "C:/custom/ykman.exe", nil
	}
	ykmanListOutput = func(path string) ([]byte, error) {
		if path != "C:/custom/ykman.exe" {
			t.Fatalf("unexpected path: %q", path)
		}
		return []byte("YubiKey 5 NFC\n"), nil
	}

	if err := CheckYubiKeyConnected(); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestCheckYubiKeyConnectedEmptyListOutput(t *testing.T) {
	prevResolve := resolveYkmanExecutableFn
	prevList := ykmanListOutput
	t.Cleanup(func() {
		resolveYkmanExecutableFn = prevResolve
		ykmanListOutput = prevList
	})

	resolveYkmanExecutableFn = func() (string, error) {
		return "C:/custom/ykman.exe", nil
	}
	ykmanListOutput = func(_ string) ([]byte, error) {
		return []byte("   \n"), nil
	}

	err := CheckYubiKeyConnected()
	if !errors.Is(err, ErrYubiKeyNotConnected) {
		t.Fatalf("expected ErrYubiKeyNotConnected, got: %v", err)
	}
}

func TestCheckYubiKeyConnectedListError(t *testing.T) {
	prevResolve := resolveYkmanExecutableFn
	prevList := ykmanListOutput
	t.Cleanup(func() {
		resolveYkmanExecutableFn = prevResolve
		ykmanListOutput = prevList
	})

	resolveYkmanExecutableFn = func() (string, error) {
		return "C:/custom/ykman.exe", nil
	}
	ykmanListOutput = func(_ string) ([]byte, error) {
		return nil, errors.New("list failed")
	}

	err := CheckYubiKeyConnected()
	if !errors.Is(err, ErrYubiKeyNotConnected) {
		t.Fatalf("expected ErrYubiKeyNotConnected, got: %v", err)
	}
}

func TestYkmanAnnotateErrorWithEmptyStderrReturnsOriginal(t *testing.T) {
	t.Parallel()
	original := errors.New("exit status 1")
	got := ykmanAnnotateError(original, "  \n")
	if got != original {
		t.Fatalf("expected original error, got: %v", got)
	}
}

func TestYkmanAnnotateErrorDetectsNoYubiKey(t *testing.T) {
	t.Parallel()
	err := ykmanAnnotateError(errors.New("exit 1"), "ERROR: No YubiKey detected.")
	if !strings.Contains(err.Error(), "no YubiKey detected") {
		t.Fatalf("expected no-yubikey message, got: %v", err)
	}
}

func TestYkmanAnnotateErrorDetectsSlotNotConfigured(t *testing.T) {
	t.Parallel()
	err := ykmanAnnotateError(errors.New("exit 1"), "ERROR: Slot is empty.")
	if !strings.Contains(err.Error(), "slot 2 is not configured") {
		t.Fatalf("expected slot-not-configured message, got: %v", err)
	}
}

func TestYkmanAnnotateErrorDetectsTimeout(t *testing.T) {
	t.Parallel()
	err := ykmanAnnotateError(errors.New("exit 1"), "Timeout waiting for response.")
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout message, got: %v", err)
	}
}

func TestYkmanAnnotateErrorUnknownIncludesStderr(t *testing.T) {
	t.Parallel()
	err := ykmanAnnotateError(errors.New("exit 1"), "Some unexpected ykman error")
	if !strings.Contains(err.Error(), "ykman stderr: Some unexpected ykman error") {
		t.Fatalf("expected stderr in error message, got: %v", err)
	}
}

func TestQueryYukeyUsesInjectableOtpCalculate(t *testing.T) {
	prevResolve := resolveYkmanExecutableFn
	prevCalc := ykmanOtpCalculate
	t.Cleanup(func() {
		resolveYkmanExecutableFn = prevResolve
		ykmanOtpCalculate = prevCalc
	})

	resolveYkmanExecutableFn = func() (string, error) { return "ykman", nil }
	wantResponse := make([]byte, 20)
	ykmanOtpCalculate = func(_, _ string) ([]byte, error) {
		return []byte(hex.EncodeToString(wantResponse) + "\n"), nil
	}

	challenge := make([]byte, challengeLen)
	resp, err := queryYubikey(challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp) != string(wantResponse) {
		t.Fatalf("response mismatch")
	}
}

func TestQueryYukeyIncludesResponseHexOnDecodeError(t *testing.T) {
	prevResolve := resolveYkmanExecutableFn
	prevCalc := ykmanOtpCalculate
	t.Cleanup(func() {
		resolveYkmanExecutableFn = prevResolve
		ykmanOtpCalculate = prevCalc
	})

	resolveYkmanExecutableFn = func() (string, error) { return "ykman", nil }
	ykmanOtpCalculate = func(_, _ string) ([]byte, error) {
		return []byte("not-hex-at-all\n"), nil
	}

	_, err := queryYubikey(make([]byte, challengeLen))
	if err == nil {
		t.Fatal("expected error for invalid hex response")
	}
	if !strings.Contains(err.Error(), "not-hex-at-all") {
		t.Fatalf("expected response hex in error message, got: %v", err)
	}
}

func TestValidateChallengeHexAcceptsValidChallenge(t *testing.T) {
	t.Parallel()
	valid := hex.EncodeToString(make([]byte, challengeLen))
	if err := ValidateChallengeHex(valid); err != nil {
		t.Fatalf("expected no error for valid challenge, got: %v", err)
	}
}

func TestValidateChallengeHexRejectsNonHex(t *testing.T) {
	t.Parallel()
	err := ValidateChallengeHex("not-hex!")
	if err == nil || !strings.Contains(err.Error(), "not valid hex") {
		t.Fatalf("expected not-valid-hex error, got: %v", err)
	}
}

func TestValidateChallengeHexRejectsWrongLength(t *testing.T) {
	t.Parallel()
	short := hex.EncodeToString(make([]byte, challengeLen-1))
	err := ValidateChallengeHex(short)
	if err == nil || !strings.Contains(err.Error(), "unexpected length") {
		t.Fatalf("expected unexpected-length error, got: %v", err)
	}
}

func TestCheckYubiKeyConnectedMissingYkman(t *testing.T) {
	prevResolve := resolveYkmanExecutableFn
	prevList := ykmanListOutput
	t.Cleanup(func() {
		resolveYkmanExecutableFn = prevResolve
		ykmanListOutput = prevList
	})

	resolveYkmanExecutableFn = func() (string, error) {
		return "", errors.New("not found")
	}
	ykmanListOutput = func(_ string) ([]byte, error) {
		t.Fatal("ykmanListOutput should not be called when ykman is unavailable")
		return nil, nil
	}

	err := CheckYubiKeyConnected()
	if !errors.Is(err, ErrYubikeyNotFound) {
		t.Fatalf("expected ErrYubikeyNotFound, got: %v", err)
	}
}
