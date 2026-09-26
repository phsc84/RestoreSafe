package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Injectable for tests.
var (
	checkYubiKeyConnectedFn = security.CheckYubiKeyConnected
	combineWithPasswordFn   = security.CombineWithPassword
	registerSpareFn         = security.RegisterSpareYubiKey
	generateRecoveryCodeFn  = security.GenerateRecoveryCode
	unlockKeySetFn          = operation.UnlockKeySet
)

// maxEnrollAttempts bounds retries of each enrollment step (password entry,
// spare registration, recovery code confirmation).
const maxEnrollAttempts = 3

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
	if reason := catalog.KeySetMismatch(cfg, ks); reason != "" {
		return keyPlan{NewKeysReason: strings.ToUpper(reason[:1]) + reason[1:]}
	}
	return keyPlan{Existing: ks}
}

// obtainKeys unlocks the planned key set or runs enrollment. The caller must
// zero the returned master key.
func obtainKeys(u ui.UI, cfg *util.Config, plan keyPlan, log *util.Logger) (*container.KeySet, []byte, error) {
	if plan.Existing != nil {
		master, err := unlockKeySetFn(u, plan.Existing, operation.UnlockOptions{PasswordPrompt: "Enter backup password: "}, log)
		if err != nil {
			return nil, nil, err
		}
		log.InfoLogOnly("Existing keys unlocked (%s).", plan.Existing.Summary())
		return plan.Existing, master, nil
	}
	return enrollKeySet(u, cfg, log)
}

// newSlot is one slot to create during enrollment, with its secret.
type newSlot struct {
	slotType  string
	label     string
	secret    []byte
	challenge *security.ChallengeData
	check     string
}

// enrollKeySet creates a new key set: a new password (unless YubiKey-only),
// YubiKey registration (plus the spare, if configured), and a recovery code
// (if configured). Every slot is checked to unlock before anything is written.
func enrollKeySet(u ui.UI, cfg *util.Config, log *util.Logger) (*container.KeySet, []byte, error) {
	out := u.Output()
	mode := int(cfg.AuthenticationMode)
	params := security.Argon2Params{
		Time:     uint32(cfg.Argon2.Time),
		MemoryKB: uint32(cfg.Argon2.MemoryMB) * 1024,
		Threads:  uint8(cfg.Argon2.Threads),
	}

	var slots []newSlot
	defer func() {
		for _, s := range slots {
			security.ZeroBytes(s.secret)
		}
	}()

	fmt.Fprintln(out, "Creating new keys for this backup directory.")
	var password []byte
	if mode != container.AuthModeYubiKey {
		pw, err := readNewPassword(u, cfg.PasswordMinLength)
		if err != nil {
			return nil, nil, err
		}
		password = pw
		defer security.ZeroBytes(password)
	} else {
		fmt.Fprintln(out, "YubiKey-only mode: no password required.")
	}

	if mode == container.AuthModePassword {
		slots = append(slots, newSlot{slotType: container.SlotPassword, label: "Password", secret: append([]byte(nil), password...)})
	} else {
		yubiSlots, err := registerYubiKeys(u, password, mode, cfg.YubiKeySpare)
		slots = append(slots, yubiSlots...)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.RecoveryCode {
		code, err := createRecoveryCode(u)
		if err != nil {
			return nil, nil, err
		}
		slots = append(slots, newSlot{slotType: container.SlotRecovery, label: "Recovery code", secret: code.Secret(), check: code.Check()})
	}

	ks, master, err := container.NewKeySet(mode)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range slots {
		if err := ks.AddSlot(master, s.slotType, s.label, s.secret, params, s.challenge, s.check); err != nil {
			security.ZeroBytes(master)
			return nil, nil, err
		}
	}

	// Prove every slot opens before any backup is written with these keys.
	for i, s := range slots {
		check, err := ks.Unlock(i, s.secret)
		if err != nil || !bytes.Equal(check, master) {
			security.ZeroBytes(master)
			return nil, nil, fmt.Errorf("Internal error: new key slot %d does not unlock. No backup was written.", i)
		}
		security.ZeroBytes(check)
	}
	if err := ks.Validate(); err != nil {
		security.ZeroBytes(master)
		return nil, nil, fmt.Errorf("Internal error: new keys are invalid: %w", err)
	}

	log.Info("New keys created (%s).", ks.Summary())
	return ks, master, nil
}

// readNewPassword asks for a new password (entered twice) of at least
// minLength characters, allowing corrections.
func readNewPassword(u ui.UI, minLength int) ([]byte, error) {
	if minLength < util.PasswordMinLengthFloor {
		minLength = util.DefaultPasswordMinLength
	}
	out := u.Output()
	for attempt := 1; attempt <= maxEnrollAttempts; attempt++ {
		pw, err := u.NewPassword(fmt.Sprintf("Enter new backup password (at least %d characters): ", minLength), "Re-enter new backup password: ")
		if err != nil {
			if errors.Is(err, security.ErrPasswordMismatch) || errors.Is(err, security.ErrPasswordEmpty) {
				fmt.Fprintf(out, "%v Please try again.\n", err)
				continue
			}
			return nil, err
		}
		if n := utf8.RuneCount(pw); n < minLength {
			security.ZeroBytes(pw)
			fmt.Fprintf(out, "The password has %d characters; at least %d are required. Please try again.\n", n, minLength)
			continue
		}
		return pw, nil
	}
	return nil, fmt.Errorf("No valid new password entered. No backup was written.")
}

// registerYubiKeys registers YubiKey 1 and, when spare is set, the spare
// YubiKey. It returns the slots registered so far even on error, so the
// caller can zero their secrets.
func registerYubiKeys(u ui.UI, password []byte, mode int, spare bool) ([]newSlot, error) {
	out := u.Output()
	slotType := container.RegularSlotType(mode)
	if err := checkYubiKeyConnectedFn(); err != nil {
		return nil, security.ErrYubiKeyRequired
	}
	fmt.Fprintln(out, "YubiKey 1:")
	fmt.Fprintln(out, "  1. Windows first asks for your YubiKey PIN to register the backup credential.")
	fmt.Fprintln(out, "  2. Windows asks again for your YubiKey PIN to derive the encryption key.")
	combined, challengeJSON, err := combineWithPasswordFn(password, mode == container.AuthModeYubiKey)
	if err != nil {
		return nil, fmt.Errorf("YubiKey authentication failed: %w", err)
	}
	primary, err := security.ParseChallengeJSON(challengeJSON)
	if err != nil {
		security.ZeroBytes(combined)
		return nil, fmt.Errorf("YubiKey registration returned invalid data: %w", err)
	}
	slots := []newSlot{{slotType: slotType, label: "YubiKey 1", secret: combined, challenge: &primary}}
	if !spare {
		return slots, nil
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Spare YubiKey: remove YubiKey 1 and insert your spare YubiKey.")
	for attempt := 1; attempt <= maxEnrollAttempts; attempt++ {
		connected, err := u.WaitForSpareYubiKey()
		if err != nil {
			return slots, err
		}
		if !connected {
			return slots, fmt.Errorf("Spare YubiKey registration cancelled. No backup was written. Remedy: Set 'yubikey_spare: false' in config.yaml to back up without a spare YubiKey.")
		}
		if err := checkYubiKeyConnectedFn(); err != nil {
			fmt.Fprintln(out, "No YubiKey detected. Insert the spare YubiKey.")
			continue
		}
		fmt.Fprintln(out, "  Windows asks twice for the PIN of the spare YubiKey (register, then derive).")
		combined, challengeJSON, err := registerSpareFn(password, primary)
		if errors.Is(err, security.ErrYubiKeyAlreadyRegistered) {
			fmt.Fprintln(out, "This is YubiKey 1. Remove it and insert your spare YubiKey.")
			continue
		}
		if err != nil {
			return slots, fmt.Errorf("Spare YubiKey registration failed: %w", err)
		}
		cd, err := security.ParseChallengeJSON(challengeJSON)
		if err != nil {
			security.ZeroBytes(combined)
			return slots, fmt.Errorf("Spare YubiKey registration returned invalid data: %w", err)
		}
		fmt.Fprintln(out, "Spare YubiKey registered. Keep it in a safe place, separate from YubiKey 1.")
		return append(slots, newSlot{slotType: slotType, label: "YubiKey 2 (spare)", secret: combined, challenge: &cd}), nil
	}
	return slots, fmt.Errorf("Spare YubiKey registration failed after %d attempts. No backup was written.", maxEnrollAttempts)
}

// createRecoveryCode shows a new recovery code once and asks the user to type
// it back, so it is only used when it has been written down correctly.
func createRecoveryCode(u ui.UI) (security.RecoveryCode, error) {
	code, err := generateRecoveryCodeFn()
	if err != nil {
		return security.RecoveryCode{}, err
	}
	out := u.Output()
	u.ShowRecoveryCode(code.String())
	for attempt := 1; attempt <= maxEnrollAttempts; attempt++ {
		answer, err := u.RetypeRecoveryCode()
		if err != nil {
			return security.RecoveryCode{}, err
		}
		typed, err := security.ParseRecoveryCode(answer)
		if err == nil && typed == code {
			fmt.Fprintln(out, "Recovery code confirmed.")
			return code, nil
		}
		if err == nil {
			err = fmt.Errorf("The code does not match the recovery code shown above.")
		}
		fmt.Fprintf(out, "%v Please check your note.\n", err)
	}
	return security.RecoveryCode{}, fmt.Errorf("Recovery code not confirmed. No backup was written.")
}
