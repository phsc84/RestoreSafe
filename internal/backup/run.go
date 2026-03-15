package backup

import (
	"RestoreSafe/internal/util"
)

// Run executes backup workflow.
func Run(cfg *util.Config, exeDir string) error {
	return RunBackup(cfg, exeDir)
}
