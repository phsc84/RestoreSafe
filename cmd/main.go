package main

import (
	"RestoreSafe/internal/backup"
	"RestoreSafe/internal/restore"
	"RestoreSafe/internal/startup"
	"RestoreSafe/internal/util"
	"RestoreSafe/internal/verify"
	"bufio"
	"fmt"
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

	// CLI flags — used when running from a batch file or task scheduler.
	//   RestoreSafe.exe -config=<abs-path>  load config from custom location
	//   RestoreSafe.exe -backup             run backup non-interactively and exit
	//   RestoreSafe.exe -restore            run restore for newest run and exit
	//   RestoreSafe.exe -verify             run verify for newest run and exit
	configPath := filepath.Join(exeDir, "config.yaml")
	cliBackup := false
	cliRestore := false
	cliVerify := false
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-backup" || arg == "--backup":
			cliBackup = true
		case arg == "-restore" || arg == "--restore":
			cliRestore = true
		case arg == "-verify" || arg == "--verify":
			cliVerify = true
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

	cliCount := 0
	if cliBackup {
		cliCount++
	}
	if cliRestore {
		cliCount++
	}
	if cliVerify {
		cliCount++
	}
	if cliCount > 1 {
		fmt.Fprintln(os.Stderr, "Error: only one of -backup, -restore, -verify may be specified at a time.")
		os.Exit(1)
	}

	cfg, err := util.Load(configPath)
	if err != nil {
		if cliCount == 1 {
			fmt.Fprintf(os.Stderr, "Error loading configuration from %s: %v\n", configPath, err)
			os.Exit(1)
		}
		exitWithError(fmt.Sprintf("Error loading configuration from %s", configPath), err)
	}

	if cliBackup && cfg.AuthenticationMode != util.AuthModeYubiKey {
		fmt.Fprintln(os.Stderr, "Error: -backup requires authentication_mode: 3 (YubiKey only). Remedy: Set authentication_mode to 3 in config.yaml or run interactive backup without -backup.")
		os.Exit(1)
	}

	startup.RunStartupHealthCheck(cfg, exeDir)

	// Non-interactive CLI mode: skip start confirmations and exit with a proper code.
	if cliCount == 1 {
		cfg.NonInteractive = true
		switch {
		case cliBackup:
			if err := backup.Run(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
				os.Exit(1)
			}
		case cliRestore:
			if err := restore.Run(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
				os.Exit(1)
			}
		case cliVerify:
			if err := verify.Run(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Verification failed: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	// Interactive menu mode.
	for {
		printMenu()
		choice := getUserInput("Select an option (1-4): ")

		switch strings.TrimSpace(choice) {
		case "1":
			if err := backup.Run(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
				waitForKeyPress()
			}
			fmt.Println()
		case "2":
			if err := restore.Run(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
				waitForKeyPress()
			}
			fmt.Println()
		case "3":
			if err := verify.Run(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Verification failed: %v\n", err)
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

func printMenu() {
	fmt.Println("========================================")
	fmt.Printf("RestoreSafe v%s\n", Version)
	fmt.Println("Secure backup application")
	fmt.Println("========================================")
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
	if err != nil && err.Error() != "EOF" {
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
	fmt.Println("Press any key to exit...")
	buf := make([]byte, 1)
	_, err := os.Stdin.Read(buf)
	if err != nil && err.Error() != "EOF" {
		fmt.Fprintf(os.Stderr, "Error reading key press: %v\n", err)
	}
}
