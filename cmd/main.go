package main

import (
	"RestoreSafe/internal/backup"
	"RestoreSafe/internal/restore"
	"RestoreSafe/internal/startup"
	"RestoreSafe/internal/util"
	"RestoreSafe/internal/verify"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Version is set at build time via -ldflags
var Version = "dev"

func main() {
	// Set working directory to the location of the executable.
	exePath, err := os.Executable()
	if err != nil {
		exitWithError("Error determining executable path", err)
	}
	exeDir := filepath.Dir(exePath)
	if err := os.Chdir(exeDir); err != nil {
		exitWithError("Error setting working directory", err)
	}

	// CLI flag for custom config path
	configPath := filepath.Join(exeDir, "config.yaml")
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-config" || arg == "--config":
			fmt.Fprintln(os.Stderr, "Error: use -config=<absolute-path-to-config.yaml> (equals form only).")
			os.Exit(1)
		case strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config="):
			idx := strings.IndexByte(arg, '=')
			flag := arg[:idx]
			value := strings.TrimSpace(arg[idx+1:])
			if value == "" {
				fmt.Fprintf(os.Stderr, "Error: %s requires a non-empty absolute path. Remedy: Pass %s=<absolute-path-to-config.yaml>.\n", flag, flag)
				os.Exit(1)
			}
			if !filepath.IsAbs(value) {
				fmt.Fprintf(os.Stderr, "Error: %s requires an absolute path. Remedy: Pass %s=<absolute-path-to-config.yaml>.\n", flag, flag)
				os.Exit(1)
			}
			configPath = filepath.Clean(value)
		}
	}

	cfg, err := util.Load(configPath)
	if err != nil {
		exitWithError(fmt.Sprintf("Error loading configuration from %s", configPath), err)
	}

	printStartupBanner(Version)
	startup.RunStartupHealthCheck(cfg, exeDir, configPath)

	// Interactive menu mode.
	for {
		printMenu()
		choice := getUserInput("Select an option (1-4): ")
		fmt.Println()

		switch strings.TrimSpace(choice) {
		case "1":
			if err := backup.Run(cfg, exeDir); err != nil {
				reportOperationError("Backup", err)
				waitForKeyPress()
			}
			fmt.Println()
		case "2":
			if err := restore.Run(cfg, exeDir); err != nil {
				reportOperationError("Restore", err)
				waitForKeyPress()
			}
			fmt.Println()
		case "3":
			if err := verify.Run(cfg, exeDir); err != nil {
				reportOperationError("Verification", err)
				waitForKeyPress()
			}
			fmt.Println()
		case "4":
			fmt.Println("Goodbye!")
			return
		default:
			fmt.Println("Invalid option. Please try again.")
			fmt.Println()
		}
	}
}

func reportOperationError(action string, err error) {
	if action == "Backup" && strings.HasPrefix(err.Error(), "Backup preflight failed:") {
		fmt.Fprintln(os.Stderr, "Backup failed.")
		fmt.Fprintln(os.Stderr)
		return
	}
	if action == "Restore" && strings.HasPrefix(err.Error(), "Restore preflight failed:") {
		fmt.Fprintln(os.Stderr, "Restore failed.")
		fmt.Fprintln(os.Stderr)
		return
	}

	fmt.Fprintf(os.Stderr, "%s failed: %v\n", action, err)
	fmt.Fprintln(os.Stderr)
}

func printStartupBanner(version string) {
	fmt.Println("========================================")
	fmt.Printf("RestoreSafe v%s\n", version)
	fmt.Println("Secure backup application")
	fmt.Println("========================================")
	fmt.Println()
}

func printMenu() {
	fmt.Println("----------------------")
	fmt.Println("Menu")
	fmt.Println("----------------------")
	fmt.Println()
	fmt.Println("1. Create backup")
	fmt.Println("2. Restore backup")
	fmt.Println("3. Verify backup")
	fmt.Println("4. Exit")
	fmt.Println()
}

func getUserInput(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
	}
	return input
}

func exitWithError(message string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	waitForKeyPress()
	os.Exit(1)
}

func waitForKeyPress() {
	fmt.Println()
	fmt.Println("Press Enter to exit.")
	buf := make([]byte, 1)
	_, err := os.Stdin.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "Error reading key press: %v\n", err)
	}
}
