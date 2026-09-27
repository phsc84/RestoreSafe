// Command RestoreSafe starts the RestoreSafe window. The optional argument
// -config=<absolute path> selects a configuration other than config.yaml
// next to the executable.
package main

import (
	"RestoreSafe/internal/gui"
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Version is injected by build.bat from versioninfo.json
// (-ldflags "-X main.Version=..."); "dev" marks an un-stamped build.
var Version = "dev"

func main() {
	exePath, err := os.Executable()
	if err != nil {
		gui.ShowError("Start failed", fmt.Sprintf("Error determining executable path: %v", err))
		os.Exit(1)
	}
	exeDir := filepath.Dir(exePath)
	if err := os.Chdir(exeDir); err != nil {
		gui.ShowError("Start failed", fmt.Sprintf("Error setting working directory: %v", err))
		os.Exit(1)
	}

	configPath, err := configPathFromArgs(os.Args[1:], exeDir)
	if err != nil {
		gui.ShowError("Invalid argument", err.Error())
		os.Exit(1)
	}

	util.AppVersion = Version
	cfg, err := util.Load(configPath)
	if err != nil {
		gui.ShowError("Configuration error", fmt.Sprintf("Error loading configuration from %s:\n\n%v", filepath.ToSlash(configPath), err))
		os.Exit(1)
	}

	if err := gui.Run(gui.Options{Version: Version, ExeDir: exeDir, ConfigPath: configPath, Config: cfg}); err != nil {
		gui.ShowError("Error", err.Error())
		os.Exit(1)
	}
}

// configPathFromArgs returns the configuration path: config.yaml next to the
// executable, or the absolute path of -config=<path>.
func configPathFromArgs(args []string, exeDir string) (string, error) {
	configPath := filepath.Join(exeDir, "config.yaml")
	for _, arg := range args {
		switch {
		case arg == "-config" || arg == "--config":
			return "", fmt.Errorf("Use -config=<absolute-path-to-config.yaml> (equals form only).")
		case strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config="):
			idx := strings.IndexByte(arg, '=')
			flag, value := arg[:idx], strings.TrimSpace(arg[idx+1:])
			if value == "" || !filepath.IsAbs(value) {
				return "", fmt.Errorf("%s requires an absolute path. Remedy: Pass %s=<absolute-path-to-config.yaml>.", flag, flag)
			}
			configPath = filepath.Clean(value)
		}
	}
	return configPath, nil
}
