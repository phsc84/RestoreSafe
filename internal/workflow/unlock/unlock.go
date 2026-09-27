// Package unlock unlocks the master keys of the key sets a run needs, by
// password, YubiKey or recovery code.
package unlock

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/security/recovery"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"fmt"
	"strings"
)

const maxPasswordAttempts = 3

// Injectable for tests.
var (
	checkYubiKeyConnectedFn = yubikey.CheckYubiKeyConnected
	deriveYubiKeySecretFn   = yubikey.DeriveFIDO2SecretAny
)

// Options configures KeySet.
type Options struct {
	// PasswordPrompt is shown when the password is needed.
	PasswordPrompt string
	// AllowRecovery offers the recovery code as an alternative when the key
	// set has one (restore and verify).
	AllowRecovery bool
}

// KeySet asks for the credential the key set requires (password,
// YubiKey, or both; optionally the recovery code) and returns the key set's
// master key. A wrong password can be retried; the YubiKey is touched only
// once. The caller must zero the returned key.
func KeySet(u interact.UI, ks *container.KeySet, opts Options, log *logging.Logger) ([]byte, error) {
	if opts.AllowRecovery && ks.HasSlotType(container.SlotRecovery) {
		useRecovery, err := u.ChooseUnlockMethod(config.AuthMode(ks.AuthMode).Label())
		if err != nil {
			return nil, err
		}
		if useRecovery {
			return unlockWithRecoveryCode(u, ks, log)
		}
	}

	slotType := container.RegularSlotType(ks.AuthMode)
	indexes := ks.SlotIndexes(slotType)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("Backup has no key slot for authentication mode %d. Remedy: Use an unmodified backup created by RestoreSafe.", ks.AuthMode)
	}

	var yubiSecret []byte
	slotIndex := indexes[0]
	if slotType != container.SlotPassword {
		idx, secret, err := deriveYubiKeySecret(u, ks, indexes)
		if err != nil {
			return nil, err
		}
		slotIndex = idx
		yubiSecret = secret
		defer cryptox.ZeroBytes(yubiSecret)
	}

	if slotType == container.SlotYubiKey {
		master, err := ks.Unlock(slotIndex, yubiSecret)
		if err != nil {
			if errors.Is(err, cryptox.ErrWrongPassword) {
				return nil, fmt.Errorf("YubiKey authentication failed: this YubiKey does not unlock the backup. Remedy: Use a YubiKey that was registered for this backup.")
			}
			return nil, err
		}
		log.InfoLogOnly("YubiKey-only authentication successful (%s).", ks.Slots[slotIndex].Label)
		return master, nil
	}

	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		password, err := u.Password(opts.PasswordPrompt)
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
				log.InfoLogOnly("YubiKey-2FA successful (%s).", ks.Slots[slotIndex].Label)
			}
			return master, nil
		}
		if !errors.Is(err, cryptox.ErrWrongPassword) {
			return nil, err
		}
		remaining := maxPasswordAttempts - attempt
		if remaining > 0 {
			fmt.Fprintf(u.Output(), "%s %d attempt(s) remaining.\n", PasswordFailurePrefix(yubiSecret != nil, false), remaining)
			log.WarnLogOnly("Wrong password or invalid second factor; attempt %d/%d", attempt, maxPasswordAttempts)
		}
	}
	if yubiSecret != nil {
		return nil, fmt.Errorf("Too many failed authentication attempts.")
	}
	return nil, fmt.Errorf("Too many wrong password attempts.")
}

func unlockWithRecoveryCode(u interact.UI, ks *container.KeySet, log *logging.Logger) ([]byte, error) {
	index := ks.SlotIndexes(container.SlotRecovery)[0]
	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		input, err := u.Password("Enter recovery code: ")
		if err != nil {
			return nil, err
		}
		code, err := recovery.Parse(string(input))
		cryptox.ZeroBytes(input)
		if err == nil && code.Check() != ks.Slots[index].Check {
			err = fmt.Errorf("This recovery code belongs to different keys. Remedy: Use the recovery code created together with these backups.")
		}
		if err == nil {
			secret := code.Secret()
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
		if remaining := maxPasswordAttempts - attempt; remaining > 0 {
			fmt.Fprintf(u.Output(), "%v %d attempt(s) remaining.\n", err, remaining)
			log.WarnLogOnly("Recovery code rejected; attempt %d/%d", attempt, maxPasswordAttempts)
		}
	}
	return nil, fmt.Errorf("Too many failed recovery code attempts.")
}

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
// made with older keys need their own credentials, which the prompt says.
func KeySets(u interact.UI, sets []catalog.SetInfo, passwordPrompt string, log *logging.Logger) (MasterKeys, error) {
	keys := make(MasterKeys)
	for _, info := range sets {
		ks := info.Header.KeySet
		if _, done := keys[ks.ID]; done {
			continue
		}
		if len(keys) > 0 {
			fmt.Fprintf(u.Output(), "Backup %s uses different keys (created %s). Authenticate with the credentials of those keys.\n", info.Entry.String(), ks.Created().Local().Format("2006-01-02"))
		}
		master, err := KeySet(u, &ks, Options{PasswordPrompt: passwordPrompt, AllowRecovery: true}, log)
		if err != nil {
			keys.Zero()
			return nil, err
		}
		keys[ks.ID] = master
	}
	return keys, nil
}

// deriveYubiKeySecret asks the connected YubiKey for the hmac-secret of the
// key set's YubiKey slots in one request: whichever registered YubiKey is
// connected answers. It returns the slot index of that YubiKey with the
// secret.
func deriveYubiKeySecret(u interact.UI, ks *container.KeySet, indexes []int) (int, []byte, error) {
	if err := checkYubiKeyConnectedFn(); err != nil {
		return 0, nil, yubikey.ErrYubiKeyRequired
	}
	challenges := make([]yubikey.ChallengeData, len(indexes))
	labels := make([]string, len(indexes))
	for i, idx := range indexes {
		challenges[i] = *ks.Slots[idx].Challenge
		labels[i] = ks.Slots[idx].Label
	}
	if len(indexes) > 1 {
		fmt.Fprintf(u.Output(), "Use any registered YubiKey (%s). Follow the on-screen prompts to authenticate.\n", strings.Join(labels, " or "))
	} else {
		fmt.Fprintln(u.Output(), "YubiKey connected. Follow the on-screen prompts to authenticate.")
	}
	i, secret, err := deriveYubiKeySecretFn(challenges)
	if err != nil {
		return 0, nil, fmt.Errorf("YubiKey authentication failed: %w", err)
	}
	return indexes[i], secret, nil
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
