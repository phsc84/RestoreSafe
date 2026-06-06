package operation

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
)

func validChallengeJSONForTest() string {
	credID := base64.StdEncoding.EncodeToString(make([]byte, 64))
	salt := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return fmt.Sprintf(`{"v":1,"nopw":false,"cred_id":%q,"salt":%q}`, credID, salt)
}

func TestOpenLoggerReturnsNonNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &util.Config{LogLevel: "info"}
	rep := util.BackupEntry{DirectoryName: "Docs", Date: "2026-03-14", ID: util.BackupID("ABC123")}

	log := OpenLogger(cfg, tmpDir, rep)
	if log == nil {
		t.Fatal("expected non-nil logger for valid target dir")
	}
	if log.IsConsoleOnly() {
		t.Fatal("expected file-backed logger for valid target dir")
	}
	log.Close()
}

func TestPasswordFailurePrefix(t *testing.T) {
	t.Parallel()
	if got := PasswordFailurePrefix(true, false); got != "Wrong password or invalid YubiKey response." {
		t.Fatalf("unexpected prefix for YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(false, false); got != "Wrong password." {
		t.Fatalf("unexpected prefix without YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(true, true); got != "Wrong YubiKey or corrupted file." {
		t.Fatalf("unexpected prefix for YubiKey-only: %q", got)
	}
}

func TestReadChallengeFileTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	challengePath := filepath.Join(dir, "sample.challenge")
	valid := validChallengeJSONForTest()
	if err := os.WriteFile(challengePath, []byte("  "+valid+"  \n"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	challenge, err := readChallengeFile(challengePath)
	if err != nil {
		t.Fatalf("readChallengeFile returned error: %v", err)
	}
	if challenge != valid {
		t.Fatalf("expected trimmed challenge JSON, got %q", challenge)
	}
}

func TestReadChallengeFileAcceptsValidJSON(t *testing.T) {
	dir := t.TempDir()
	challengePath := filepath.Join(dir, "sample.challenge")
	valid := validChallengeJSONForTest()
	if err := os.WriteFile(challengePath, []byte(valid), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	challenge, err := readChallengeFile(challengePath)
	if err != nil {
		t.Fatalf("expected no error for valid JSON, got: %v", err)
	}
	if challenge != valid {
		t.Fatalf("expected challenge JSON, got %q", challenge)
	}
}

func TestReadChallengeFileRejectsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	challengePath := filepath.Join(dir, "empty.challenge")
	if err := os.WriteFile(challengePath, []byte("  "), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	_, err := readChallengeFile(challengePath)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-file error, got: %v", err)
	}
}

func TestReadChallengeFileRejectsNonHex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	challengePath := filepath.Join(dir, "bad.challenge")
	if err := os.WriteFile(challengePath, []byte("not-hex-content"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	_, err := readChallengeFile(challengePath)
	if err == nil || !strings.Contains(err.Error(), "invalid format") {
		t.Fatalf("expected invalid-format error, got: %v", err)
	}
}

func TestReadChallengeFileRejectsWrongLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	challengePath := filepath.Join(dir, "short.challenge")
	if err := os.WriteFile(challengePath, []byte("abcd"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	_, err := readChallengeFile(challengePath)
	if err == nil || !strings.Contains(err.Error(), "invalid format") {
		t.Fatalf("expected invalid-format error for short hex, got: %v", err)
	}
}

func TestVerifyPasswordAcceptsCorrectAndRejectsWrong(t *testing.T) {
	dir := t.TempDir()
	partPath := filepath.Join(dir, "part-001.enc")
	password := []byte("restore-safe")

	f, err := os.Create(partPath)
	if err != nil {
		t.Fatalf("failed to create encrypted part: %v", err)
	}
	if err := security.Encrypt(f, bytes.NewReader([]byte("payload")), password, security.DefaultArgon2Params); err != nil {
		f.Close()
		t.Fatalf("failed to encrypt payload: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close encrypted part: %v", err)
	}

	if err := verifyPassword(partPath, password); err != nil {
		t.Fatalf("verifyPassword should accept correct password, got: %v", err)
	}

	err = verifyPassword(partPath, []byte("wrong"))
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword for invalid password, got: %v", err)
	}
}

func TestVerifyPasswordMissingFile(t *testing.T) {
	t.Parallel()
	err := verifyPassword(filepath.Join(t.TempDir(), "missing.enc"), []byte("pw"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to open file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromptStartActionPrintsSingleBlankLineBeforeAndAfterPrompt(t *testing.T) {
	prevReadLine := readLineFn
	prevStdout := os.Stdout
	t.Cleanup(func() {
		readLineFn = prevReadLine
		os.Stdout = prevStdout
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	readLineFn = func(prompt string) (string, error) {
		if prompt != "Start verification now? [Y/n]: " {
			t.Fatalf("unexpected prompt: %q", prompt)
		}
		return "y", nil
	}

	confirmed, err := PromptStartAction("verification")
	if err != nil {
		t.Fatalf("PromptStartAction returned error: %v", err)
	}
	if !confirmed {
		t.Fatal("expected confirmation true")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close write end: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close read end: %v", err)
	}

	if got := string(out); got != "\n\n" {
		t.Fatalf("expected exactly one empty line before and after prompt, got %q", got)
	}
}

func TestPromptStartActionPrintsSingleBlankLineOnRetry(t *testing.T) {
	prevReadLine := readLineFn
	prevStdout := os.Stdout
	t.Cleanup(func() {
		readLineFn = prevReadLine
		os.Stdout = prevStdout
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	call := 0
	readLineFn = func(prompt string) (string, error) {
		if prompt != "Start verification now? [Y/n]: " {
			t.Fatalf("unexpected prompt: %q", prompt)
		}
		if call == 0 {
			call++
			return "maybe", nil
		}
		return "n", nil
	}

	confirmed, err := PromptStartAction("verification")
	if err != nil {
		t.Fatalf("PromptStartAction returned error: %v", err)
	}
	if confirmed {
		t.Fatal("expected confirmation false")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close write end: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close read end: %v", err)
	}

	got := string(out)
	expected := "\n\nPlease enter y (yes) or n (no).\n\n\n"
	if got != expected {
		t.Fatalf("unexpected stdout.\nexpected: %q\n     got: %q", expected, got)
	}
}
