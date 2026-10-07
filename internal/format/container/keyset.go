package container

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/security/recovery"
	"RestoreSafe/internal/security/yubikey"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Slot types.
const (
	SlotPassword        = "password"
	SlotPasswordYubiKey = "password_yubikey"
	SlotYubiKey         = "yubikey"
	SlotRecovery        = "recovery"
)

const (
	keySetIDLen   = 16
	kdfAlgArgon2  = "argon2id"
	wrappedKeyLen = cryptox.KeyLen + 16 // master key + GCM tag
	slotAADLabel  = "RestoreSafe v2 slot"
	infoData      = "RestoreSafe v2 data"
	infoManifest  = "RestoreSafe v2 manifest"
)

// KDF holds the Argon2id parameters and salt of one slot.
type KDF struct {
	Alg       string `json:"alg"`
	Salt      []byte `json:"salt"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint32 `json:"threads"`
}

func (k KDF) params() cryptox.Argon2Params {
	return cryptox.Argon2Params{Time: k.Time, MemoryKB: k.MemoryKiB, Threads: uint8(k.Threads)}
}

// Slot is one way to unlock a key set: it holds the key set's master key
// encrypted with a key derived from one credential.
type Slot struct {
	Type      string                 `json:"type"`
	Label     string                 `json:"label"`
	KDF       KDF                    `json:"kdf"`
	Challenge *yubikey.ChallengeData `json:"challenge,omitempty"`
	Check     string                 `json:"check,omitempty"`
	Nonce     []byte                 `json:"nonce"`
	Wrapped   []byte                 `json:"wrapped"`
}

// UsesYubiKey reports whether unlocking this slot requires a YubiKey.
func (s Slot) UsesYubiKey() bool {
	return s.Type == SlotPasswordYubiKey || s.Type == SlotYubiKey
}

// UsesPassword reports whether unlocking this slot requires the password.
func (s Slot) UsesPassword() bool {
	return s.Type == SlotPassword || s.Type == SlotPasswordYubiKey
}

// KeySet is a random master key plus the slots that each hold a wrapped copy
// of it. It is created once at enrollment and copied unchanged into the header
// of every set written with it.
type KeySet struct {
	ID         string          `json:"id"`
	CreatedUTC string          `json:"created_utc"`
	AuthMode   config.AuthMode `json:"auth_mode"`
	Slots      []Slot          `json:"slots"`
}

// NewKeySet creates an empty key set and its random master key. The caller
// adds slots with AddSlot and must zero the master key after use.
func NewKeySet(authMode config.AuthMode) (*KeySet, []byte, error) {
	id, err := cryptox.RandomBytes(keySetIDLen)
	if err != nil {
		return nil, nil, err
	}
	master, err := cryptox.RandomBytes(cryptox.KeyLen)
	if err != nil {
		return nil, nil, err
	}
	ks := &KeySet{
		ID:         hex.EncodeToString(id),
		CreatedUTC: time.Now().UTC().Format(time.RFC3339Nano),
		AuthMode:   authMode,
	}
	return ks, master, nil
}

// Created returns CreatedUTC parsed as a time. Validate guarantees it parses.
func (ks *KeySet) Created() time.Time {
	t, _ := time.Parse(time.RFC3339, ks.CreatedUTC)
	return t
}

// AddSlot wraps master with a key derived from secret and appends the slot.
// challenge is required for YubiKey slots; check is required for the recovery
// slot.
func (ks *KeySet) AddSlot(master []byte, slotType, label string, secret []byte, params cryptox.Argon2Params, challenge *yubikey.ChallengeData, check string) error {
	salt, err := cryptox.RandomBytes(cryptox.SaltLen)
	if err != nil {
		return err
	}
	nonce, err := cryptox.RandomBytes(cryptox.NonceLen)
	if err != nil {
		return err
	}
	kek, err := cryptox.DeriveKEK(secret, salt, params)
	if err != nil {
		return err
	}
	defer cryptox.ZeroBytes(kek)

	index := len(ks.Slots)
	wrapped, err := cryptox.SealKey(kek, nonce, master, slotAAD(ks.ID, index, slotType))
	if err != nil {
		return err
	}
	ks.Slots = append(ks.Slots, Slot{
		Type:      slotType,
		Label:     label,
		KDF:       KDF{Alg: kdfAlgArgon2, Salt: salt, Time: params.Time, MemoryKiB: params.MemoryKB, Threads: uint32(params.Threads)},
		Challenge: challenge,
		Check:     check,
		Nonce:     nonce,
		Wrapped:   wrapped,
	})
	return nil
}

// Unlock derives the key-encryption key of slot index from secret and returns
// the master key. A wrong credential returns cryptox.ErrWrongPassword.
func (ks *KeySet) Unlock(index int, secret []byte) ([]byte, error) {
	if index < 0 || index >= len(ks.Slots) {
		return nil, fmt.Errorf("Internal error: key slot %d does not exist.", index)
	}
	slot := ks.Slots[index]
	kek, err := cryptox.DeriveKEK(secret, slot.KDF.Salt, slot.KDF.params())
	if err != nil {
		return nil, err
	}
	defer cryptox.ZeroBytes(kek)
	return cryptox.OpenKey(kek, slot.Nonce, slot.Wrapped, slotAAD(ks.ID, index, slot.Type))
}

// SlotIndexes returns the indexes of all slots of the given type.
func (ks *KeySet) SlotIndexes(slotType string) []int {
	var out []int
	for i, s := range ks.Slots {
		if s.Type == slotType {
			out = append(out, i)
		}
	}
	return out
}

// RegularSlotType returns the slot type used for the key set's authentication
// mode (the non-recovery slots).
func RegularSlotType(authMode config.AuthMode) string {
	switch authMode {
	case config.AuthModePasswordYubiKey:
		return SlotPasswordYubiKey
	case config.AuthModeYubiKey:
		return SlotYubiKey
	default:
		return SlotPassword
	}
}

// HasSlotType reports whether the key set has at least one slot of slotType.
func (ks *KeySet) HasSlotType(slotType string) bool {
	return len(ks.SlotIndexes(slotType)) > 0
}

// YubiKeyCount returns the number of registered YubiKey slots.
func (ks *KeySet) YubiKeyCount() int {
	n := 0
	for _, s := range ks.Slots {
		if s.UsesYubiKey() {
			n++
		}
	}
	return n
}

func slotAAD(keySetID string, index int, slotType string) []byte {
	aad := make([]byte, 0, len(slotAADLabel)+len(keySetID)+4+len(slotType))
	aad = append(aad, slotAADLabel...)
	aad = append(aad, keySetID...)
	aad = binary.BigEndian.AppendUint32(aad, uint32(index))
	return append(aad, slotType...)
}

// Validate checks the key set structure (not the secrets).
func (ks *KeySet) Validate() error {
	if raw, err := hex.DecodeString(ks.ID); err != nil || len(raw) != keySetIDLen {
		return headerErr("invalid key set ID")
	}
	if _, err := time.Parse(time.RFC3339, ks.CreatedUTC); err != nil {
		return headerErr("invalid key set creation time")
	}
	regular := RegularSlotType(ks.AuthMode)
	if ks.AuthMode < config.AuthModePassword || ks.AuthMode > config.AuthModeYubiKey {
		return headerErr("invalid authentication mode %d", ks.AuthMode)
	}
	if len(ks.Slots) == 0 {
		return headerErr("key set has no slots")
	}
	regularCount, recoveryCount := 0, 0
	for i, s := range ks.Slots {
		switch s.Type {
		case regular:
			regularCount++
		case SlotRecovery:
			recoveryCount++
		default:
			return headerErr("slot %d has type %q, which does not match authentication mode %d", i, s.Type, ks.AuthMode)
		}
		if err := validateSlot(s); err != nil {
			return headerErr("slot %d: %v", i, err)
		}
	}
	if regularCount < 1 || recoveryCount > 1 {
		return headerErr("unexpected slot counts")
	}
	if regular == SlotPassword && regularCount != 1 {
		return headerErr("password mode must have exactly one password slot")
	}
	if regular != SlotPassword && regularCount > 2 {
		return headerErr("at most two YubiKey slots are supported")
	}
	// All YubiKeys of a key set share one hmac-secret salt so that restore can
	// ask for any of them in a single request.
	salt := ""
	for _, s := range ks.Slots {
		if !s.UsesYubiKey() {
			continue
		}
		if salt != "" && s.Challenge.Salt != salt {
			return headerErr("YubiKey slots use different salts")
		}
		salt = s.Challenge.Salt
	}
	return nil
}

// Summary returns a one-line description such as
// "created 2026-09-01, password + YubiKey (2 YubiKeys), recovery code".
func (ks *KeySet) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "created %s, %s", ks.Created().Local().Format("2006-01-02"), ks.AuthMode.Label())
	if n := ks.YubiKeyCount(); n > 0 {
		fmt.Fprintf(&b, " (%d YubiKey", n)
		if n > 1 {
			b.WriteString("s")
		}
		b.WriteString(")")
	}
	if ks.HasSlotType(SlotRecovery) {
		b.WriteString(", recovery code")
	}
	return b.String()
}

func validateSlot(s Slot) error {
	if s.Label == "" || len(s.Label) > 100 {
		return fmt.Errorf("invalid label")
	}
	if s.KDF.Alg != kdfAlgArgon2 || len(s.KDF.Salt) != cryptox.SaltLen {
		return fmt.Errorf("invalid key derivation settings")
	}
	if err := cryptox.ValidateArgon2Params(s.KDF.Time, s.KDF.MemoryKiB, s.KDF.Threads, "in backup header", "Remedy: Use an unmodified backup created by RestoreSafe."); err != nil {
		return err
	}
	if len(s.Nonce) != cryptox.NonceLen || len(s.Wrapped) != wrappedKeyLen {
		return fmt.Errorf("invalid wrapped key")
	}
	if s.UsesYubiKey() {
		if s.Challenge == nil {
			return fmt.Errorf("missing YubiKey challenge")
		}
		raw, err := json.Marshal(s.Challenge)
		if err != nil {
			return err
		}
		if err := yubikey.ValidateChallengeJSON(string(raw)); err != nil {
			return err
		}
		if s.Challenge.NoPassword != (s.Type == SlotYubiKey) {
			return fmt.Errorf("YubiKey challenge does not match the slot type")
		}
	} else if s.Challenge != nil {
		return fmt.Errorf("unexpected YubiKey challenge")
	}
	if s.Type == SlotRecovery {
		if !recovery.ValidCheck(s.Check) {
			return fmt.Errorf("invalid recovery check field")
		}
	} else if s.Check != "" {
		return fmt.Errorf("unexpected recovery check field")
	}
	return nil
}

// SectionKeys are the per-set keys of the data and manifest sections.
type SectionKeys struct {
	Data     []byte
	Manifest []byte
}

// Zero overwrites both keys.
func (k *SectionKeys) Zero() {
	if k == nil {
		return
	}
	cryptox.ZeroBytes(k.Data)
	cryptox.ZeroBytes(k.Manifest)
}

// DeriveSectionKeys derives the section keys of one set from the key set
// master key and the set's header hash.
func DeriveSectionKeys(master, headerHash []byte) (*SectionKeys, error) {
	data, err := cryptox.DeriveSubkey(master, headerHash, infoData)
	if err != nil {
		return nil, err
	}
	man, err := cryptox.DeriveSubkey(master, headerHash, infoManifest)
	if err != nil {
		cryptox.ZeroBytes(data)
		return nil, err
	}
	return &SectionKeys{Data: data, Manifest: man}, nil
}
