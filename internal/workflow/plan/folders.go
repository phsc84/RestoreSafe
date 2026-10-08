package plan

import (
	"fmt"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
)

// Folder says whether one source directory gets a full or a differential
// backup in this run.
type Folder struct {
	// Base is the full backup a differential is based on; nil means a full
	// backup.
	Base *catalog.SetInfo
	// DiffNumber is the number of the new differential.
	DiffNumber int
	// Reason explains the decision for the preflight and the log.
	Reason string
}

// IsDiff reports whether the plan is a differential backup.
func (p *Folder) IsDiff() bool { return p != nil && p.Base != nil }

// Label returns "Differential" or "Full".
func (p *Folder) Label() string {
	if p.IsDiff() {
		return "Differential"
	}
	return "Full"
}

// Folders decides per source directory between a full and a differential
// backup (2.0 spec 6.1), keyed by backup name. All checks use headers and
// trailers only, so the complete plan is known before the password is asked.
// forceFull (the user chose a full backup) makes every directory a full backup.
func Folders(cfg *config.Config, infos []catalog.SetInfo, sources []Source, keys Keys, forceFull bool, now time.Time) map[string]*Folder {
	plans := make(map[string]*Folder)
	for _, src := range sources {
		if src.Err != nil || src.Skip {
			continue
		}
		name := src.BackupName
		if name == "" {
			name = naming.DirectoryBaseName(src.Resolved)
		}
		plans[name] = planFolder(cfg, infos, name, keys, forceFull, now)
	}
	return plans
}

func planFolder(cfg *config.Config, infos []catalog.SetInfo, directory string, keys Keys, forceFull bool, now time.Time) *Folder {
	full := func(reason string) *Folder { return &Folder{Reason: reason} }

	switch {
	case forceFull:
		return full("full backup requested")
	case keys.Existing == nil:
		return full("new keys")
	case !cfg.Differential.IsEnabled():
		return full("differential backups disabled in config.yaml")
	}

	// infos are sorted newest first, so the first complete full is the base.
	var base *catalog.SetInfo
	for i := range infos {
		info := &infos[i]
		if info.Entry.DirectoryName == directory && !info.Entry.IsDiff() && info.Complete() {
			base = info
			break
		}
	}
	if base == nil {
		return full("no complete full backup found")
	}
	if base.Header.KeySet.ID != keys.Existing.ID {
		return full("the latest full backup uses older keys")
	}

	ageDays := int(now.Sub(base.Created()).Hours() / 24)
	limit := cfg.Differential.IntervalDays()
	if ageDays >= limit {
		return full(fmt.Sprintf("full backup is %d days old (limit %d)", ageDays, limit))
	}

	// Differential numbers are never reused: count every differential of the
	// chain, complete or not.
	maxNumber := 0
	var newestDiff *catalog.SetInfo
	for i := range infos {
		info := &infos[i]
		if info.Entry.DirectoryName != directory || info.Entry.ChainID != base.Entry.ChainID || !info.Entry.IsDiff() {
			continue
		}
		if info.Entry.DiffNumber > maxNumber {
			maxNumber = info.Entry.DiffNumber
		}
		if newestDiff == nil && info.Complete() {
			newestDiff = info
		}
	}
	if maxNumber >= naming.MaxDiffNumber {
		return full(fmt.Sprintf("chain %s reached the maximum of %d differentials", base.Entry.ChainID, naming.MaxDiffNumber))
	}
	if newestDiff != nil && base.Trailer.DataLength > 0 {
		percent := int(newestDiff.Trailer.DataLength * 100 / base.Trailer.DataLength)
		if limit := cfg.Differential.SizePercent(); percent >= limit {
			return full(fmt.Sprintf("last differential was %d%% of the full backup (limit %d%%)", percent, limit))
		}
	}

	return &Folder{
		Base:       base,
		DiffNumber: maxNumber + 1,
		Reason:     fmt.Sprintf("base: full %s %s, %d days old", base.Entry.Date, base.Entry.ChainID, ageDays),
	}
}

// AnyDifferential reports whether at least one directory gets a differential.
func AnyDifferential(plans map[string]*Folder) bool {
	for _, p := range plans {
		if p.IsDiff() {
			return true
		}
	}
	return false
}
