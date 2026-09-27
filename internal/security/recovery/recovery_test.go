package recovery

import (
	"strings"
	"testing"
)

func TestGenerateRecoveryCodeFormatAndRoundTrip(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		s := code.String()
		if len(s) != 35 || strings.Count(s, "-") != 5 {
			t.Fatalf("unexpected display form %q", s)
		}
		if seen[s] {
			t.Fatalf("duplicate recovery code %q", s)
		}
		seen[s] = true

		parsed, err := Parse(s)
		if err != nil || parsed != code {
			t.Fatalf("round trip of %q failed: %v", s, err)
		}
		if !ValidCheck(code.Check()) || len(code.Secret()) != 25 {
			t.Fatalf("unexpected check/secret for %q", s)
		}
	}
}

func TestParseRecoveryCodeNormalizesInput(t *testing.T) {
	t.Parallel()

	code, _ := Generate()
	s := code.String()
	variants := []string{
		strings.ToLower(s),
		strings.ReplaceAll(s, "-", " "),
		strings.ReplaceAll(s, "-", ""),
		"  " + s + "  ",
	}
	for _, v := range variants {
		if got, err := Parse(v); err != nil || got != code {
			t.Fatalf("variant %q not accepted: %v", v, err)
		}
	}
	// Crockford decoding: O reads as 0, I and L as 1.
	if strings.ContainsAny(s, "01") {
		confusable := strings.NewReplacer("0", "O", "1", "l").Replace(s)
		if got, err := Parse(confusable); err != nil || got != code {
			t.Fatalf("confusable variant %q not accepted: %v", confusable, err)
		}
	}
}

func TestParseRecoveryCodeRejectsTyposAndBadInput(t *testing.T) {
	t.Parallel()

	code, _ := Generate()
	s := []byte(code.String())
	// Change the first data character to a different valid character.
	if s[0] == 'A' {
		s[0] = 'B'
	} else {
		s[0] = 'A'
	}
	if _, err := Parse(string(s)); err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	if _, err := Parse("ABCDE-FGHJK"); err == nil || !strings.Contains(err.Error(), "must have 30 characters") {
		t.Fatalf("expected length error, got %v", err)
	}
	if _, err := Parse(code.String()[:34] + "U"); err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("expected invalid-character error, got %v", err)
	}
}
