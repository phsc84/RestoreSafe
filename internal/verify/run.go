package verify

import (
	"RestoreSafe/internal/util"
)

// Run executes verify workflow.
func Run(cfg *util.Config, exeDir string) error {
	return RunVerify(cfg, exeDir)
}
