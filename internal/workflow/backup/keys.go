package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/problem"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/security/recovery"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/plan"
	"RestoreSafe/internal/workflow/unlock"
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Injectable for tests.
var (
	checkYubiKeyConnectedFn = yubikey.CheckConnected
	combineWithPasswordFn   = yubikey.CombineWithPassword
	registerSpareFn         = yubikey.RegisterSpare
	generateRecoveryCodeFn  = recovery.Generate
	unlockKeySetFn          = unlock.KeySet
)

// maxEnrollAttempts bounds retries of each enrollment step (password entry,
// spare registration, recovery code confirmation).
const maxEnrollAttempts = 3

// obtainKeys unlocks the planned key set or runs enrollment. The caller must
// zero the returned master key.
func obtainKeys(u interact.UI, cfg *config.Config, keys plan.Keys, log *logging.Logger) (*container.KeySet, []byte, error) {
	if keys.Existing != nil {
		master, err := unlockKeySetFn(u, keys.Existing, unlock.Options{PasswordPrompt: "Enter backup password: "}, log)
		if err != nil {
			return nil, nil, err
		}
		log.InfoLogOnly("Existing keys unlocked (%s).", keys.Existing.Summary())
		return keys.Existing, master, nil
	}
	return enrollKeySet(u, cfg, log)
}

// newSlot is one slot to create during enrollment, with its secret.
type newSlot struct {
	slotType  string
	label     string
	secret    []byte
	challenge *yubikey.ChallengeData
	check     string
}

// enrollKeySet creates a new key set: a new password (unless YubiKey-only),
// YubiKey registration (plus the spare, if configured), and a recovery code
// (if configured). Every slot is checked to unlock before anything is written.
func enrollKeySet(u interact.UI, cfg *config.Config, log *logging.Logger) (*container.KeySet, []byte, error) {
	out := u.Output()
	mode := cfg.AuthenticationMode
	params := cryptox.Argon2Params{
		Time:     uint32(cfg.Argon2.Time),
		MemoryKB: uint32(cfg.Argon2.MemoryMB) * 1024,
		Threads:  uint8(cfg.Argon2.Threads),
	}

	var slots []newSlot
	defer func() {
		for _, s := range slots {
			cryptox.ZeroBytes(s.secret)
		}
	}()

	fmt.Fprintln(out, "Creating new keys for this backup directory.")
	var password []byte
	if mode != config.AuthModeYubiKey {
		pw, err := readNewPassword(u, cfg.PasswordMinLength)
		if err != nil {
			return nil, nil, err
		}
		password = pw
		defer cryptox.ZeroBytes(password)
	} else {
		fmt.Fprintln(out, "YubiKey-only mode: no password required.")
	}

	if mode == config.AuthModePassword {
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
			cryptox.ZeroBytes(master)
			return nil, nil, err
		}
	}

	// Prove every slot opens before any backup is written with these keys.
	for i, s := range slots {
		check, err := ks.Unlock(i, s.secret)
		if err != nil || !bytes.Equal(check, master) {
			cryptox.ZeroBytes(master)
			return nil, nil, fmt.Errorf("Internal error: new key slot %d does not unlock. No backup was written.", i)
		}
		cryptox.ZeroBytes(check)
	}
	if err := ks.Validate(); err != nil {
		cryptox.ZeroBytes(master)
		return nil, nil, fmt.Errorf("Internal error: new keys are invalid: %w", err)
	}

	log.Info("New keys created (%s).", ks.Summary())
	return ks, master, nil
}

// readNewPassword asks for a new password (entered twice) of at least
// minLength characters, allowing corrections.
func readNewPassword(u interact.UI, minLength int) ([]byte, error) {
	if minLength < config.PasswordMinLengthFloor {
		minLength = config.DefaultPasswordMinLength
	}
	out := u.Output()
	for attempt := 1; attempt <= maxEnrollAttempts; attempt++ {
		pw, err := u.NewPassword(fmt.Sprintf("Enter new backup password (at least %d characters): ", minLength), "Re-enter new backup password: ")
		if err != nil {
			if errors.Is(err, interact.ErrPasswordMismatch) || errors.Is(err, interact.ErrPasswordEmpty) {
				fmt.Fprintf(out, "%v Please try again.\n", err)
				continue
			}
			return nil, err
		}
		if n := utf8.RuneCount(pw); n < minLength {
			cryptox.ZeroBytes(pw)
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
func registerYubiKeys(u interact.UI, password []byte, mode config.AuthMode, spare bool) ([]newSlot, error) {
	out := u.Output()
	slotType := container.RegularSlotType(mode)
	if err := checkYubiKeyConnectedFn(); err != nil {
		return nil, yubikey.ErrRequired
	}
	fmt.Fprintln(out, "YubiKey 1:")
	fmt.Fprintln(out, "  1. Windows first asks for your YubiKey PIN to register the backup credential.")
	fmt.Fprintln(out, "  2. Windows asks again for your YubiKey PIN to derive the encryption key.")
	combined, challengeJSON, err := combineWithPasswordFn(password, mode == config.AuthModeYubiKey)
	if err != nil {
		return nil, fmt.Errorf("YubiKey authentication failed: %w", err)
	}
	primary, err := yubikey.ParseChallengeJSON(challengeJSON)
	if err != nil {
		cryptox.ZeroBytes(combined)
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
			return slots, problem.New("Spare YubiKey registration cancelled. No backup was written.").WithRemedy("Set 'yubikey_spare: false' in config.yaml to back up without a spare YubiKey.")
		}
		if err := checkYubiKeyConnectedFn(); err != nil {
			fmt.Fprintln(out, "No YubiKey detected. Insert the spare YubiKey.")
			continue
		}
		fmt.Fprintln(out, "  Windows asks twice for the PIN of the spare YubiKey (register, then derive).")
		combined, challengeJSON, err := registerSpareFn(password, primary)
		if errors.Is(err, yubikey.ErrAlreadyRegistered) {
			fmt.Fprintln(out, "This is YubiKey 1. Remove it and insert your spare YubiKey.")
			continue
		}
		if err != nil {
			return slots, fmt.Errorf("Spare YubiKey registration failed: %w", err)
		}
		cd, err := yubikey.ParseChallengeJSON(challengeJSON)
		if err != nil {
			cryptox.ZeroBytes(combined)
			return slots, fmt.Errorf("Spare YubiKey registration returned invalid data: %w", err)
		}
		fmt.Fprintln(out, "Spare YubiKey registered. Keep it in a safe place, separate from YubiKey 1.")
		return append(slots, newSlot{slotType: slotType, label: "YubiKey 2 (spare)", secret: combined, challenge: &cd}), nil
	}
	return slots, fmt.Errorf("Spare YubiKey registration failed after %d attempts. No backup was written.", maxEnrollAttempts)
}

// createRecoveryCode creates a new recovery code and shows it once.
func createRecoveryCode(u interact.UI) (recovery.Code, error) {
	code, err := generateRecoveryCodeFn()
	if err != nil {
		return recovery.Code{}, err
	}
	if err := u.ShowRecoveryCode(code.String()); err != nil {
		return recovery.Code{}, err
	}
	return code, nil
}
