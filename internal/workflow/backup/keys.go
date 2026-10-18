package backup

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
	"github.com/phsc84/restoresafe/internal/security/recovery"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"
)

// Injectable for tests.
var (
	checkYubiKeyConnectedFn = yubikey.CheckConnected
	countYubiKeysFn         = yubikey.CountConnected
	combineWithPasswordFn   = yubikey.CombineWithPassword
	generateRecoveryCodeFn  = recovery.Generate
	unlockKeySetFn          = unlock.KeySet
)

// maxEnrollAttempts bounds retries of each enrollment step (password entry,
// recovery code confirmation).
const maxEnrollAttempts = 3

// obtainKeys unlocks the planned key set or runs enrollment. The caller must
// zero the returned master key.
func obtainKeys(u interact.UI, cfg *config.Config, keys plan.Keys, log *logging.Logger) (*container.KeySet, []byte, error) {
	if keys.Existing != nil {
		master, err := unlockKeySetFn(u, keys.Existing, unlock.Options{Action: "backup"}, log)
		if err != nil {
			return nil, nil, err
		}
		log.InfoLogOnly("Existing keys unlocked (%s).", keys.Existing.Summary())
		return keys.Existing, master, nil
	}
	return enrollKeySet(u, cfg, log)
}

// yubiKeyRefusedReason is why the plan after a YubiKey refusal creates new
// keys.
const yubiKeyRefusedReason = "Your YubiKey didn't unlock the current keys"

// askAfterYubiKeyRefusal asks what to do when unlocking the existing keys
// failed with err. Only a YubiKey that didn't unlock them is asked about: a
// YubiKey that was reset or is another one can never unlock them again, and
// new keys are the way on. Any other failure, or a cancelled question,
// answers RefusalCancel, and the backup ends with err.
func askAfterYubiKeyRefusal(u interact.UI, keys plan.Keys, err error, log *logging.Logger) interact.YubiKeyRefusalAnswer {
	var yk *unlock.YubiKeyError
	if keys.Existing == nil || !errors.As(err, &yk) {
		return interact.RefusalCancel
	}
	log.WarnLogOnly("%v", err)
	answer, qerr := u.YubiKeyRefused(interact.YubiKeyRefusal{RecoveryCode: keys.Existing.HasSlotType(container.SlotRecovery)})
	if qerr != nil {
		return interact.RefusalCancel
	}
	switch answer {
	case interact.RefusalRetry:
		log.InfoLogOnly("Trying the YubiKey again.")
	case interact.RefusalNewKeys:
		log.Info("New keys requested after the YubiKey didn't unlock the current keys.")
	}
	return answer
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
// YubiKey registration, and a recovery code (if configured, and always with a
// YubiKey). Every slot is checked to unlock before anything is written.
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
		slot, err := registerYubiKey(u, password, mode)
		if err != nil {
			return nil, nil, err
		}
		slots = append(slots, slot)
	}

	if cfg.CreatesRecoveryCode() {
		code, err := createRecoveryCode(u)
		if err != nil {
			return nil, nil, err
		}
		slots = append(slots, newSlot{slotType: container.SlotRecovery, label: "Recovery code", secret: code.Secret(), check: code.Check()})
		code.Zero()
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

// enrollAttempt describes try n of a question while keys are created,
// after failure.
func enrollAttempt(n int, failure error) interact.Attempt {
	return interact.Attempt{N: n, Left: maxEnrollAttempts - n + 1, Failure: failure}
}

// readNewPassword asks for a new password (entered twice) of at least
// minLength characters, allowing corrections.
func readNewPassword(u interact.UI, minLength int) ([]byte, error) {
	if minLength < config.PasswordMinLengthFloor {
		minLength = config.DefaultPasswordMinLength
	}
	var failure error
	for attempt := 1; attempt <= maxEnrollAttempts; attempt++ {
		pw, err := u.NewPassword(interact.NewPasswordQuestion{MinLength: minLength, Attempt: enrollAttempt(attempt, failure)})
		if err != nil {
			if errors.Is(err, interact.ErrPasswordMismatch) || errors.Is(err, interact.ErrPasswordEmpty) {
				failure = err
				continue
			}
			return nil, err
		}
		if n := utf8.RuneCount(pw); n < minLength {
			cryptox.ZeroBytes(pw)
			failure = fmt.Errorf("The password has %d characters; at least %d are required.", n, minLength)
			continue
		}
		return pw, nil
	}
	return nil, fmt.Errorf("No valid new password entered. No backup was written.")
}

// registerYubiKey registers the connected YubiKey: Windows asks for its PIN
// twice, to register the credential and to derive the secret.
func registerYubiKey(u interact.UI, password []byte, mode config.AuthMode) (newSlot, error) {
	out := u.Output()
	if err := checkYubiKeyConnectedFn(); err != nil {
		return newSlot{}, yubikey.ErrRequired
	}
	fmt.Fprintln(out, "YubiKey:")
	fmt.Fprintln(out, "  1. Windows first asks for your YubiKey PIN to register the backup credential.")
	fmt.Fprintln(out, "  2. Windows asks again for your YubiKey PIN to derive the encryption key.")
	combined, challengeJSON, err := combineWithPasswordFn(password, mode == config.AuthModeYubiKey)
	if err != nil {
		return newSlot{}, fmt.Errorf("YubiKey authentication failed: %w", err)
	}
	cd, err := yubikey.ParseChallengeJSON(challengeJSON)
	if err != nil {
		cryptox.ZeroBytes(combined)
		return newSlot{}, fmt.Errorf("YubiKey registration returned invalid data: %w", err)
	}
	return newSlot{slotType: container.RegularSlotType(mode), label: "YubiKey", secret: combined, challenge: &cd}, nil
}

// createRecoveryCode creates a new recovery code and shows it once.
func createRecoveryCode(u interact.UI) (recovery.Code, error) {
	code, err := generateRecoveryCodeFn()
	if err != nil {
		return recovery.Code{}, err
	}
	display := code.Display()
	err = u.ShowRecoveryCode(display)
	cryptox.ZeroBytes(display)
	if err != nil {
		code.Zero()
		return recovery.Code{}, err
	}
	return code, nil
}
