package restore

import (
	"strings"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/job"
)

// restorePlan describes the restore for the user, from the values the
// details report was built from.
func restorePlan(items []restorePreflightItem, restorePath string, ks *container.KeySet, details interact.Report) interact.RestorePlan {
	p := interact.RestorePlan{
		Destination: restorePath,
		NeededBytes: estimateRestoreBytes(items),
		FreeBytes:   -1,
		Unlock:      job.UnlockPlan(ks),
		Issues:      details.Issues,
		Details:     details,
	}
	if free, err := queryRestoreTargetFreeBytes(restorePath); err == nil {
		p.FreeBytes = int64(free)
	}
	for _, item := range items {
		sp := interact.RestoreSetPlan{SetPlan: item.SetPlan(), OutputDir: item.OutputDir}
		if item.OutputDirErr != nil {
			sp.OutputProblem, sp.OutputRemedy = problem.Split(item.OutputDirErr)
			sp.OutputCode = item.OutputDirCode
		}
		p.Sets = append(p.Sets, sp)
	}
	return p
}

// PlanDestination is the plan of restoring sets into destination, without
// asking anything: the same checks Run makes before it asks to start (the
// folders to create, the space, whether a YubiKey is connected). infos is
// the inventory of backupDir. The Restore backup window checks the choices with it
// while the user makes them.
func PlanDestination(cfg *config.Config, backupDir string, infos []catalog.SetInfo, sets []naming.BackupEntry, destination string) (interact.RestorePlan, error) {
	if strings.TrimSpace(destination) == "" {
		return interact.RestorePlan{}, problem.New("No restore destination chosen.").WithRemedy("Choose the folder to restore into.")
	}
	selected, err := job.SelectSets(infos, sets)
	if err != nil {
		return interact.RestorePlan{}, err
	}
	items := buildRestorePreflight(selected, infos, destination)
	first := selected[0].Header
	details := restorePreflightReport(cfg, backupDir, destination, items, first.KeySet.AuthMode, yubikey.CheckConnected)
	return restorePlan(items, destination, &first.KeySet, details), nil
}
