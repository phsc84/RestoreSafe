package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bytes"
	"fmt"
	"strings"
)

// Injectable for tests.
var (
	readPasswordConfirmedFn = security.ReadPasswordConfirmedWithPrompts
	checkYubiKeyConnectedFn = security.CheckYubiKeyConnected
	combineWithPasswordFn   = security.CombineWithPassword
	unlockKeySetFn          = operation.UnlockKeySet
)

// keyPlan describes whether a backup run reuses the current key set or
// creates new keys (enrollment).
type keyPlan struct {
	// Existing is the key set to reuse; nil means new keys are created.
	Existing *container.KeySet
	// NewKeysReason explains why new keys are created.
	NewKeysReason string
}

// planKeys selects the key set of the newest complete backup when it matches
// the configuration; otherwise new keys are needed.
func planKeys(cfg *util.Config, infos []catalog.SetInfo) keyPlan {
	ks := catalog.CurrentKeySet(infos)
	if ks == nil {
		return keyPlan{NewKeysReason: "No existing keys found in the backup directory"}
	}
	if ks.AuthMode != int(cfg.AuthenticationMode) {
		return keyPlan{NewKeysReason: "authentication_mode changed in config.yaml"}
	}
	return keyPlan{Existing: ks}
}

// describeKeySet returns a one-line summary such as
// "created 2026-09-01, password + YubiKey (1 YubiKey)".
func describeKeySet(ks *container.KeySet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "created %s, %s", ks.Created().Local().Format("2006-01-02"), util.AuthMode(ks.AuthMode).Label())
	if n := ks.YubiKeyCount(); n > 0 {
		fmt.Fprintf(&b, " (%d YubiKey", n)
		if n > 1 {
			b.WriteString("s")
		}
		b.WriteString(")")
	}
	if ks.HasSlotType(container.SlotRecovery) {
		b.WriteString(", recovery code")
	}
	return b.String()
}

// obtainKeys unlocks the planned key set or runs enrollment. The caller must
// zero the returned master key.
func obtainKeys(cfg *util.Config, plan keyPlan, log *util.Logger) (*container.KeySet, []byte, error) {
	if plan.Existing != nil {
		master, err := unlockKeySetFn(plan.Existing, "Enter backup password: ", log)
		if err != nil {
			return nil, nil, err
		}
		log.InfoLogOnly("Existing keys unlocked (%s).", describeKeySet(plan.Existing))
		return plan.Existing, master, nil
	}
	return enrollKeySet(cfg, log)
}

// enrollKeySet creates a new key set: it asks for a new password (unless in
// YubiKey-only mode), registers the YubiKey, and checks that every slot
// unlocks before anything is written.
func enrollKeySet(cfg *util.Config, log *util.Logger) (*container.KeySet, []byte, error) {
	mode := int(cfg.AuthenticationMode)
	params := security.Argon2Params{
		Time:     uint32(cfg.Argon2.Time),
		MemoryKB: uint32(cfg.Argon2.MemoryMB) * 1024,
		Threads:  uint8(cfg.Argon2.Threads),
	}

	fmt.Println("Creating new keys for this backup directory.")
	var password []byte
	if mode != container.AuthModeYubiKey {
		pw, err := readPasswordConfirmedFn("Enter new backup password: ", "Re-enter new backup password: ")
		if err != nil {
			return nil, nil, err
		}
		password = pw
		defer security.ZeroBytes(password)
	} else {
		fmt.Println("YubiKey-only mode: no password required.")
	}

	ks, master, err := container.NewKeySet(mode)
	if err != nil {
		return nil, nil, err
	}
	ok := false
	defer func() {
		if !ok {
			security.ZeroBytes(master)
		}
	}()

	secret := password
	var challenge *security.ChallengeData
	if mode != container.AuthModePassword {
		if err := checkYubiKeyConnectedFn(); err != nil {
			return nil, nil, security.ErrYubiKeyRequired
		}
		fmt.Println("YubiKey interaction:")
		fmt.Println("  1. Windows first asks for your YubiKey PIN to register the backup credential.")
		fmt.Println("  2. Windows asks again for your YubiKey PIN to derive the encryption key.")
		combined, challengeJSON, err := combineWithPasswordFn(password, mode == container.AuthModeYubiKey)
		if err != nil {
			return nil, nil, fmt.Errorf("YubiKey authentication failed: %w", err)
		}
		defer security.ZeroBytes(combined)
		cd, err := security.ParseChallengeJSON(challengeJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("YubiKey registration returned invalid data: %w", err)
		}
		secret = combined
		challenge = &cd
	}

	slotType := container.RegularSlotType(mode)
	label := "Password"
	if slotType != container.SlotPassword {
		label = "YubiKey 1"
	}
	if err := ks.AddSlot(master, slotType, label, secret, params, challenge, ""); err != nil {
		return nil, nil, err
	}

	// Prove every slot opens before any backup is written with these keys.
	for i := range ks.Slots {
		check, err := ks.Unlock(i, secret)
		if err != nil || !bytes.Equal(check, master) {
			return nil, nil, fmt.Errorf("Internal error: new key slot %d does not unlock. No backup was written.", i)
		}
		security.ZeroBytes(check)
	}
	if err := ks.Validate(); err != nil {
		return nil, nil, fmt.Errorf("Internal error: new keys are invalid: %w", err)
	}

	log.Info("New keys created (%s).", describeKeySet(ks))
	ok = true
	return ks, master, nil
}
