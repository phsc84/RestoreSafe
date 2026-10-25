// Package unlock unlocks the master keys of the key sets a run needs, by
// password, YubiKey or recovery code.
package unlock

import (
	"errors"
	"fmt"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
	"github.com/phsc84/restoresafe/internal/security/recovery"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

const maxPasswordAttempts = 3

// Injectable for tests.
var (
	checkYubiKeyConnectedFn = yubikey.CheckConnected
	deriveYubiKeySecretFn   = yubikey.DeriveFIDO2Secret
)

// Options configures KeySet.
type Options struct {
	// Action is the operation that needs the keys: "backup", "restore" or
	// "verification".
	Action string
	// AllowRecovery offers the recovery code as an alternative when the key
	// set has one (restore and verify).
	AllowRecovery bool
	// Keys is set when the operation unlocked another key set before.
	Keys *interact.OtherKeys
}

// KeySet asks for the credential the key set requires (password,
// YubiKey, or both; optionally the recovery code) and returns the key set's
// master key. A wrong password can be retried; the YubiKey is touched only
// once. The caller must zero the returned key.
func KeySet(u interact.UI, ks *container.KeySet, opts Options, log *logging.Logger) ([]byte, error) {
	if opts.AllowRecovery && ks.HasSlotType(container.SlotRecovery) {
		useRecovery, err := u.ChooseUnlockMethod(interact.UnlockChoice{Mode: ks.AuthMode, Action: opts.Action, Keys: opts.Keys})
		if err != nil {
			return nil, err
		}
		if useRecovery {
			return unlockWithRecoveryCode(u, ks, opts, log)
		}
	}

	slotType := container.RegularSlotType(ks.AuthMode)
	indexes := ks.SlotIndexes(slotType)
	if len(indexes) == 0 {
		return nil, problem.Errorf("Backup has no key slot for authentication mode %d.", ks.AuthMode).WithRemedy("Use an unmodified backup created by RestoreSafe.")
	}

	var yubiSecret []byte
	slotIndex := indexes[0]
	if slotType != container.SlotPassword {
		secret, err := deriveYubiKeySecret(u, ks.Slots[slotIndex])
		if err != nil {
			return nil, err
		}
		yubiSecret = secret
		defer cryptox.ZeroBytes(yubiSecret)
	}

	if slotType == container.SlotYubiKey {
		master, err := ks.Unlock(slotIndex, yubiSecret)
		if err != nil {
			if errors.Is(err, cryptox.ErrWrongPassword) {
				return nil, &YubiKeyError{Err: problem.New("YubiKey authentication failed: this YubiKey does not unlock the backup.").WithRemedy("Use a YubiKey that was registered for this backup.")}
			}
			return nil, err
		}
		log.Info("YubiKey-only authentication successful (%s).", ks.Slots[slotIndex].Label)
		return master, nil
	}

	var failure error
	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		password, err := u.Password(interact.SecretQuestion{Kind: interact.SecretPassword, Action: opts.Action, Keys: opts.Keys, Attempt: attemptOf(attempt, failure)})
		if err != nil {
			return nil, err
		}
		secret := password
		if yubiSecret != nil {
			secret = yubikey.CombinePasswordWithSecret(password, yubiSecret)
			cryptox.ZeroBytes(password)
		}
		master, err := ks.Unlock(slotIndex, secret)
		cryptox.ZeroBytes(secret)
		if err == nil {
			if yubiSecret != nil {
				log.Info("YubiKey-2FA successful (%s).", ks.Slots[slotIndex].Label)
			}
			return master, nil
		}
		if !errors.Is(err, cryptox.ErrWrongPassword) {
			return nil, err
		}
		failure = errors.New(PasswordFailurePrefix(yubiSecret != nil, false))
		if attempt < maxPasswordAttempts {
			log.Warn("Wrong password or invalid second factor; attempt %d/%d", attempt, maxPasswordAttempts)
		}
	}
	if yubiSecret != nil {
		return nil, fmt.Errorf("Too many failed authentication attempts.")
	}
	return nil, fmt.Errorf("Too many wrong password attempts.")
}

// attemptOf describes try n of a credential question, after failure.
func attemptOf(n int, failure error) interact.Attempt {
	return interact.Attempt{N: n, Left: maxPasswordAttempts - n + 1, Failure: failure}
}

func unlockWithRecoveryCode(u interact.UI, ks *container.KeySet, opts Options, log *logging.Logger) ([]byte, error) {
	index := ks.SlotIndexes(container.SlotRecovery)[0]
	var failure error
	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		input, err := u.Password(interact.SecretQuestion{Kind: interact.SecretRecoveryCode, Action: opts.Action, Keys: opts.Keys, Attempt: attemptOf(attempt, failure)})
		if err != nil {
			return nil, err
		}
		code, err := recovery.Parse(input)
		cryptox.ZeroBytes(input)
		if err == nil && code.Check() != ks.Slots[index].Check {
			code.Zero()
			err = problem.New("This recovery code belongs to different keys.").WithRemedy("Use the recovery code created together with these backups.")
		}
		if err == nil {
			secret := code.Secret()
			code.Zero()
			master, unlockErr := ks.Unlock(index, secret)
			cryptox.ZeroBytes(secret)
			if unlockErr == nil {
				log.Info("Unlocked with the recovery code.")
				return master, nil
			}
			if !errors.Is(unlockErr, cryptox.ErrWrongPassword) {
				return nil, unlockErr
			}
			err = fmt.Errorf("Wrong recovery code.")
		}
		failure = err
		if attempt < maxPasswordAttempts {
			log.Warn("Recovery code rejected; attempt %d/%d", attempt, maxPasswordAttempts)
		}
	}
	return nil, fmt.Errorf("Too many failed recovery code attempts.")
}

// YubiKeyError is a YubiKey that didn't unlock a key set: Windows refused
// it, or the secret it derived isn't the key set's. Windows reports a
// YubiKey that doesn't know the credential (reset, or another one), a wrong
// or blocked PIN and a cancelled prompt with the same HRESULT, so they can't
// be told apart.
type YubiKeyError struct{ Err error }

func (e *YubiKeyError) Error() string { return e.Err.Error() }
func (e *YubiKeyError) Unwrap() error { return e.Err }

// MasterKeys maps key set IDs to unlocked master keys.
type MasterKeys map[string][]byte

// Zero overwrites every master key.
func (m MasterKeys) Zero() {
	for _, k := range m {
		cryptox.ZeroBytes(k)
	}
}

// KeySets unlocks every distinct key set used by sets. Usually all
// selected backups share one key set, so the user authenticates once; backups
// made with older keys need their own credentials, which the question names.
func KeySets(u interact.UI, sets []catalog.SetInfo, action string, log *logging.Logger) (MasterKeys, error) {
	keys := make(MasterKeys)
	for _, info := range sets {
		ks := info.Header.KeySet
		if _, done := keys[ks.ID]; done {
			continue
		}
		opts := Options{Action: action, AllowRecovery: true}
		if len(keys) > 0 {
			opts.Keys = &interact.OtherKeys{Set: info.Entry, Created: ks.Created()}
		}
		master, err := KeySet(u, &ks, opts, log)
		if err != nil {
			keys.Zero()
			return nil, err
		}
		keys[ks.ID] = master
	}
	return keys, nil
}

// deriveYubiKeySecret asks the connected YubiKey for the hmac-secret of the
// key set's YubiKey slot.
func deriveYubiKeySecret(u interact.UI, slot container.Slot) ([]byte, error) {
	if err := checkYubiKeyConnectedFn(); err != nil {
		return nil, yubikey.ErrRequired
	}
	secret, err := deriveYubiKeySecretFn(*slot.Challenge)
	if err != nil {
		return nil, &YubiKeyError{Err: fmt.Errorf("YubiKey authentication failed: %w", err)}
	}
	return secret, nil
}

func PasswordFailurePrefix(requiresYubiKey, yubiKeyOnly bool) string {
	switch {
	case yubiKeyOnly:
		return "Wrong YubiKey or corrupted file."
	case requiresYubiKey:
		return "Wrong password or invalid YubiKey response."
	default:
		return "Wrong password."
	}
}
