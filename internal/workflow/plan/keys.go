package plan

import (
	"strings"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
)

// Keys says whether a backup run reuses the current key set or creates new
// keys (enrollment).
type Keys struct {
	// Existing is the key set to reuse; nil means new keys are created.
	Existing *container.KeySet
	// NewKeysReason explains why new keys are created.
	NewKeysReason string
}

// KeysFor selects the key set of the newest complete backup when it matches
// the configuration; otherwise new keys are needed.
func KeysFor(cfg *config.Config, infos []catalog.SetInfo) Keys {
	ks := catalog.CurrentKeySet(infos)
	if ks == nil {
		return Keys{NewKeysReason: "No existing keys found in the backup directory"}
	}
	if reason := catalog.KeySetMismatch(cfg, ks); reason != "" {
		return Keys{NewKeysReason: strings.ToUpper(reason[:1]) + reason[1:]}
	}
	return Keys{Existing: ks}
}
