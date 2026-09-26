package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"encoding/json"
	"errors"
	"fmt"
)

const maxPasswordAttempts = 3

// Injectable for tests.
var (
	readPasswordFn          = security.ReadPassword
	checkYubiKeyConnectedFn = security.CheckYubiKeyConnected
	deriveYubiKeySecretFn   = security.DeriveFIDO2SecretForRestore
)

// UnlockKeySet asks for the credential the key set requires (password,
// YubiKey, or both) and returns the key set's master key. A wrong password
// can be retried; the YubiKey is touched only once. The caller must zero the
// returned key.
func UnlockKeySet(ks *container.KeySet, passwordPrompt string, log *util.Logger) ([]byte, error) {
	slotType := container.RegularSlotType(ks.AuthMode)
	indexes := ks.SlotIndexes(slotType)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("Backup has no key slot for authentication mode %d. Remedy: Use an unmodified backup created by RestoreSafe.", ks.AuthMode)
	}

	var yubiSecret []byte
	slotIndex := indexes[0]
	if slotType != container.SlotPassword {
		idx, secret, err := deriveYubiKeySecret(ks, indexes)
		if err != nil {
			return nil, err
		}
		slotIndex = idx
		yubiSecret = secret
		defer security.ZeroBytes(yubiSecret)
	}

	if slotType == container.SlotYubiKey {
		master, err := ks.Unlock(slotIndex, yubiSecret)
		if err != nil {
			if errors.Is(err, security.ErrWrongPassword) {
				return nil, fmt.Errorf("YubiKey authentication failed: this YubiKey does not unlock the backup. Remedy: Use the YubiKey that was registered for this backup.")
			}
			return nil, err
		}
		log.InfoLogOnly("YubiKey-only authentication successful.")
		return master, nil
	}

	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		password, err := readPasswordFn(passwordPrompt)
		if err != nil {
			return nil, err
		}
		secret := password
		if yubiSecret != nil {
			secret = security.CombinePasswordWithSecret(password, yubiSecret)
			security.ZeroBytes(password)
		}
		master, err := ks.Unlock(slotIndex, secret)
		security.ZeroBytes(secret)
		if err == nil {
			if yubiSecret != nil {
				log.InfoLogOnly("YubiKey-2FA successful.")
			}
			return master, nil
		}
		if !errors.Is(err, security.ErrWrongPassword) {
			return nil, err
		}
		remaining := maxPasswordAttempts - attempt
		if remaining > 0 {
			fmt.Printf("%s %d attempt(s) remaining.\n", PasswordFailurePrefix(yubiSecret != nil, false), remaining)
			log.WarnLogOnly("Wrong password or invalid second factor; attempt %d/%d", attempt, maxPasswordAttempts)
		}
	}
	if yubiSecret != nil {
		return nil, fmt.Errorf("Too many failed authentication attempts.")
	}
	return nil, fmt.Errorf("Too many wrong password attempts.")
}

// MasterKeys maps key set IDs to unlocked master keys.
type MasterKeys map[string][]byte

// Zero overwrites every master key.
func (m MasterKeys) Zero() {
	for _, k := range m {
		security.ZeroBytes(k)
	}
}

// UnlockKeySets unlocks every distinct key set used by sets. Usually all
// selected backups share one key set, so the user authenticates once; backups
// made with older keys need their own credentials, which the prompt says.
func UnlockKeySets(sets []catalog.SetInfo, passwordPrompt string, log *util.Logger) (MasterKeys, error) {
	keys := make(MasterKeys)
	for _, info := range sets {
		ks := info.Header.KeySet
		if _, done := keys[ks.ID]; done {
			continue
		}
		if len(keys) > 0 {
			fmt.Printf("Backup %s uses different keys (created %s). Authenticate with the credentials of those keys.\n", info.Entry.String(), ks.Created().Local().Format("2006-01-02"))
		}
		master, err := UnlockKeySet(&ks, passwordPrompt, log)
		if err != nil {
			keys.Zero()
			return nil, err
		}
		keys[ks.ID] = master
	}
	return keys, nil
}

// deriveYubiKeySecret asks the connected YubiKey for the hmac-secret of the
// key set's YubiKey slot and returns the slot index with the secret.
func deriveYubiKeySecret(ks *container.KeySet, indexes []int) (int, []byte, error) {
	if err := checkYubiKeyConnectedFn(); err != nil {
		return 0, nil, security.ErrYubiKeyRequired
	}
	idx := indexes[0]
	raw, err := json.Marshal(ks.Slots[idx].Challenge)
	if err != nil {
		return 0, nil, fmt.Errorf("Failed to encode YubiKey challenge: %w", err)
	}
	fmt.Println("YubiKey connected. Follow the on-screen prompts to authenticate.")
	secret, err := deriveYubiKeySecretFn(string(raw))
	if err != nil {
		return 0, nil, fmt.Errorf("YubiKey authentication failed: %w", err)
	}
	return idx, secret, nil
}
