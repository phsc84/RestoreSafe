package restore

import (
	"RestoreSafe/internal/util"
)

// Run executes restore workflow.
func Run(cfg *util.Config, exeDir string) error {
	return RunRestore(cfg, exeDir)
}
