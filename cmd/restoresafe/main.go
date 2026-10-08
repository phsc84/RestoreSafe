// Command RestoreSafe starts the RestoreSafe window. The optional argument
// -config=<absolute path> selects a configuration other than config.yaml
// next to the executable.
package main

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/gui"
	"RestoreSafe/internal/problem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func main() {
	// Load DLLs from System32 only: RestoreSafe is a portable exe, often run
	// from a download or USB folder, where a planted DLL must never be loaded
	// (refactoring 2.0 RF-52). Windows 8 and later have the call.
	windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32) //nolint:errcheck // nothing safer to fall back to
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

	cfg, err := config.Load(configPath)
	if err != nil {
		gui.ShowError("Configuration error", fmt.Sprintf("Error loading configuration from %s:\n\n%v", filepath.ToSlash(configPath), err))
		os.Exit(1)
	}

	if err := gui.Run(gui.Options{ExeDir: exeDir, ConfigPath: configPath, Config: cfg}); err != nil {
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
				return "", problem.Errorf("%s requires an absolute path.", flag).WithRemedy(fmt.Sprintf("Pass %s=<absolute-path-to-config.yaml>.", flag))
			}
			configPath = filepath.Clean(value)
		}
	}
	return configPath, nil
}
