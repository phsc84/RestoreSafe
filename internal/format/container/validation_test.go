package container

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/manifest"
	"RestoreSafe/internal/security/cryptox"
	"strings"
	"testing"
)

// fakeSlot is a structurally valid slot without real key material, so that
// no test needs Argon2 to build one.
func fakeSlot(slotType, label string) Slot {
	return Slot{
		Type:    slotType,
		Label:   label,
		KDF:     KDF{Alg: kdfAlgArgon2, Salt: make([]byte, cryptox.SaltLen), Time: cryptox.MinArgonTime, MemoryKiB: cryptox.MinArgonMemoryKB, Threads: cryptox.MinArgonThreads},
		Nonce:   make([]byte, cryptox.NonceLen),
		Wrapped: make([]byte, wrappedKeyLen),
	}
}

// yubiKeySet is a valid key set of mode 2: two YubiKeys and a recovery code.
func yubiKeySet() KeySet {
	one, two := fakeSlot(SlotPasswordYubiKey, "YubiKey 1"), fakeSlot(SlotPasswordYubiKey, "YubiKey 2 (spare)")
	one.Challenge, two.Challenge = testChallenge(false), testChallenge(false)
	rec := fakeSlot(SlotRecovery, "Recovery code")
	rec.Check = "ABCDE"
	return KeySet{ID: strings.Repeat("ab", keySetIDLen), CreatedUTC: "2026-09-01T10:00:00Z", AuthMode: config.AuthModePasswordYubiKey, Slots: []Slot{one, two, rec}}
}

// TestKeySetValidateRejectsEveryInconsistency breaks each rule of
// KeySet.Validate and validateSlot once, on an otherwise valid key set.
func TestKeySetValidateRejectsEveryInconsistency(t *testing.T) {
	t.Parallel()

	if ks := yubiKeySet(); ks.Validate() != nil {
		t.Fatalf("valid key set rejected: %v", ks.Validate())
	}
	cases := []struct {
		name   string
		mutate func(*KeySet)
		want   string
	}{
		{"ID", func(ks *KeySet) { ks.ID = "xyz" }, "invalid key set ID"},
		{"creation time", func(ks *KeySet) { ks.CreatedUTC = "yesterday" }, "creation time"},
		{"authentication mode", func(ks *KeySet) { ks.AuthMode = 9 }, "authentication mode 9"},
		{"two recovery slots", func(ks *KeySet) { ks.Slots = append(ks.Slots, ks.Slots[2]) }, "slot counts"},
		{"only a recovery slot", func(ks *KeySet) { ks.Slots = ks.Slots[2:] }, "slot counts"},
		{"three YubiKeys", func(ks *KeySet) { ks.Slots = append(ks.Slots, ks.Slots[0]) }, "at most two YubiKey slots"},
		{"different salts", func(ks *KeySet) {
			c := *ks.Slots[1].Challenge
			c.Salt = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWY="
			ks.Slots[1].Challenge = &c
		}, "different salts"},
		{"empty label", func(ks *KeySet) { ks.Slots[0].Label = "" }, "invalid label"},
		{"long label", func(ks *KeySet) { ks.Slots[0].Label = strings.Repeat("x", 101) }, "invalid label"},
		{"KDF algorithm", func(ks *KeySet) { ks.Slots[0].KDF.Alg = "scrypt" }, "key derivation settings"},
		{"salt length", func(ks *KeySet) { ks.Slots[0].KDF.Salt = make([]byte, 8) }, "key derivation settings"},
		{"Argon2 time", func(ks *KeySet) { ks.Slots[0].KDF.Time = cryptox.MaxArgonTime + 1 }, "Argon2 time"},
		{"nonce length", func(ks *KeySet) { ks.Slots[0].Nonce = make([]byte, 4) }, "invalid wrapped key"},
		{"wrapped length", func(ks *KeySet) { ks.Slots[0].Wrapped = make([]byte, 4) }, "invalid wrapped key"},
		{"missing challenge", func(ks *KeySet) { ks.Slots[0].Challenge = nil }, "missing YubiKey challenge"},
		{"invalid challenge", func(ks *KeySet) {
			c := *ks.Slots[0].Challenge
			c.CredID = ""
			ks.Slots[0].Challenge = &c
		}, "slot 0"},
		{"challenge without password in mode 2", func(ks *KeySet) { ks.Slots[0].Challenge = testChallenge(true) }, "does not match the slot type"},
		{"challenge on the recovery slot", func(ks *KeySet) { ks.Slots[2].Challenge = testChallenge(false) }, "unexpected YubiKey challenge"},
		{"recovery check", func(ks *KeySet) { ks.Slots[2].Check = "abc" }, "invalid recovery check"},
		{"check on a YubiKey slot", func(ks *KeySet) { ks.Slots[0].Check = "ABCDE" }, "unexpected recovery check"},
	}
	for _, tc := range cases {
		ks := yubiKeySet()
		tc.mutate(&ks)
		if err := ks.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}

	// Password mode has exactly one password slot.
	pw := KeySet{ID: strings.Repeat("ab", keySetIDLen), CreatedUTC: "2026-09-01T10:00:00Z", AuthMode: config.AuthModePassword,
		Slots: []Slot{fakeSlot(SlotPassword, "Password"), fakeSlot(SlotPassword, "Password")}}
	if err := pw.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one password slot") {
		t.Errorf("two password slots: got %v", err)
	}
}

// TestHeaderValidateRejectsDifferentialAndDateFields covers the rules of
// Header.Validate that TestHeaderValidation leaves out.
func TestHeaderValidateRejectsDifferentialAndDateFields(t *testing.T) {
	t.Parallel()

	ks := yubiKeySet()
	diff := func() *Header {
		h, err := NewHeader(manifest.SetTypeDiff, "ABC123", "DEF456", "Documents", "2026-09-26", ks)
		if err != nil {
			t.Fatal(err)
		}
		h.DiffNumber, h.BaseDate, h.BaseManifestSHA256 = 1, "2026-09-01", strings.Repeat("a", 64)
		return h
	}
	if err := diff().Validate(); err != nil {
		t.Fatalf("valid differential header rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Header)
		want   string
	}{
		{"differential number", func(h *Header) { h.DiffNumber = 1000 }, "differential number 1000"},
		{"base manifest hash", func(h *Header) { h.BaseManifestSHA256 = "abc" }, "base manifest hash"},
		{"set type", func(h *Header) { h.SetType = "incremental" }, "unknown set type"},
		{"date", func(h *Header) { h.Date = "26.09.2026" }, "invalid date"},
		{"creation time", func(h *Header) { h.CreatedUTC = "now" }, "invalid creation time"},
	}
	for _, tc := range cases {
		h := diff()
		tc.mutate(h)
		if err := h.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
	if h := diff(); !h.IsDiff() || h.Created().IsZero() {
		t.Errorf("IsDiff %v, Created %v", h.IsDiff(), h.Created())
	}
}

// TestTrailerCheckRejectsEveryInconsistency breaks each rule of the trailer
// check once.
func TestTrailerCheckRejectsEveryInconsistency(t *testing.T) {
	t.Parallel()

	valid := Trailer{DataOffset: 100, DataLength: 50, ManifestOffset: 150, ManifestLength: 40, PartCount: 1}
	size := int64(150 + 40 + TrailerLen)
	if err := valid.check(100, size, 1); err != nil {
		t.Fatalf("valid trailer rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Trailer)
		parts  int
		want   string
	}{
		{"data offset", func(tr *Trailer) { tr.DataOffset = 99 }, 1, "does not start after the header"},
		{"short section", func(tr *Trailer) { tr.DataLength = 0; tr.ManifestOffset = 100 }, 1, "section too short"},
		{"gap between sections", func(tr *Trailer) { tr.ManifestOffset = 151; tr.ManifestLength = 39 }, 1, "not adjacent"},
		{"size", func(tr *Trailer) { tr.ManifestLength = 41 }, 1, "does not match the trailer"},
		{"part count", func(*Trailer) {}, 2, "expects 1 part file(s), found 2"},
	}
	for _, tc := range cases {
		tr := valid
		tc.mutate(&tr)
		if err := tr.check(100, size, tc.parts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}

func TestKeySetQueriesAndSummary(t *testing.T) {
	t.Parallel()

	ks := yubiKeySet()
	if got := ks.SlotIndexes(SlotPasswordYubiKey); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("SlotIndexes = %v", got)
	}
	if !ks.HasSlotType(SlotRecovery) || ks.HasSlotType(SlotYubiKey) || ks.YubiKeyCount() != 2 {
		t.Errorf("HasSlotType or YubiKeyCount wrong for %+v", ks)
	}
	if !ks.Slots[0].UsesPassword() || ks.Slots[2].UsesPassword() {
		t.Error("UsesPassword: a password + YubiKey slot needs the password, a recovery slot doesn't")
	}
	if ks.Created().IsZero() {
		t.Error("Created must parse CreatedUTC")
	}
	if s := ks.Summary(); !strings.HasPrefix(s, "created 2026-09-01, ") || !strings.Contains(s, "(2 YubiKeys), recovery code") {
		t.Errorf("Summary = %q", s)
	}
	if got := RegularSlotType(config.AuthModeYubiKey); got != SlotYubiKey {
		t.Errorf("RegularSlotType(3) = %q", got)
	}
}
