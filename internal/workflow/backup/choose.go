package backup

import (
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
)

// planMode is the kind of plan the user looks at.
type planMode int

const (
	// planAutomatic chooses full or differential per directory (core spec 6.1).
	planAutomatic planMode = iota
	// planFull makes every directory a full backup with the current keys.
	planFull
	// planNewKeys creates new keys and makes every directory a full backup.
	planNewKeys
)

// choosePlan shows the plan and asks whether to start. Choosing full backups
// or new keys shows the changed plan and asks again, until the user starts
// or cancels; start is false when the user cancelled. The sources are
// measured once, for the automatic plan. A blocked automatic plan returns
// the error of its first failed check.
func choosePlan(u interact.UI, cfg *config.Config, backupDir string, sources []plan.Source, infos []catalog.SetInfo) (plan.Keys, map[string]*plan.Folder, bool, error) {
	now := time.Now()
	current := plan.KeysFor(cfg, infos)
	automatic := plan.Folders(cfg, infos, sources, current, false, now)
	sizes := measureSources(cfg, backupDir, sources, automatic)
	partErr := validateBackupPartCount(cfg, sources)

	mode := planAutomatic
	for {
		keys, folders := current, automatic
		switch mode {
		case planFull:
			folders = plan.Folders(cfg, infos, sources, keys, true, now)
		case planNewKeys:
			keys = plan.Keys{NewKeysReason: "New keys requested"}
			folders = plan.Folders(cfg, infos, sources, keys, true, now)
		}

		est := sizes.estimate(folders)
		details := backupPreflightReport(cfg, backupDir, sources, keys, folders, est, yubikey.CheckConnected)
		issues, blockErr := backupPreflightIssues(backupDir, sources, est, partErr)
		details.Issues = issues
		removes := plan.RetentionPreview(cfg, infos, sources, folders, now)
		u.ShowBackupPlan(backupPlan(cfg, backupDir, sources, keys, folders, est, removes, mode != planAutomatic, details))
		if blockErr != nil && mode == planAutomatic {
			return keys, folders, false, blockErr
		}

		choice, err := u.ConfirmBackupStart(interact.BackupStartOptions{
			Blocked:        blockErr != nil,
			OfferFull:      mode == planAutomatic && plan.AnyDifferential(folders),
			OfferNewKeys:   mode != planNewKeys && current.Existing != nil,
			OfferAutomatic: mode != planAutomatic,
		})
		if err != nil {
			return keys, folders, false, err
		}
		switch choice {
		case interact.BackupAsPlanned:
			if blockErr != nil {
				return keys, folders, false, blockErr
			}
			return keys, folders, true, nil
		case interact.BackupFull:
			mode = planFull
		case interact.BackupNewKeys:
			mode = planNewKeys
		case interact.BackupAutomatic:
			mode = planAutomatic
		default:
			return keys, folders, false, nil
		}
	}
}
