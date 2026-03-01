package main

import (
	"RestoreSafe/internal/engine"
	"RestoreSafe/internal/util"
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// Set working directory to the location of the executable
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error determining executable path: %v\n", err)
		waitForKeyPress()
		os.Exit(1)
	}
	exeDir := filepath.Dir(exePath)
	if err := os.Chdir(exeDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting working directory: %v\n", err)
		waitForKeyPress()
		os.Exit(1)
	}

	cfg, err := util.Load(filepath.Join(exeDir, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		waitForKeyPress()
		os.Exit(1)
	}

	// Handle command-line arguments if provided
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "-backup":
			if err := engine.RunBackup(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
				waitForKeyPress()
				os.Exit(1)
			}
			waitForKeyPress()
			return
		case "-restore":
			if err := engine.RunRestore(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
				waitForKeyPress()
				os.Exit(1)
			}
			waitForKeyPress()
			return
		}
	}

	// Interactive menu mode
	for {
		printMenu()
		choice := getUserInput("Select an option (1-3): ")

		switch strings.TrimSpace(choice) {
		case "1":
			if err := engine.RunBackup(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
			}
			fmt.Println()
		case "2":
			if err := engine.RunRestore(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
				waitForKeyPress()
			}
			fmt.Println()
		case "3":
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
	fmt.Println("RestoreSafe - Secure backup application")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("1. Create backup")
	fmt.Println("2. Restore backup")
	fmt.Println("3. Exit")
	fmt.Println()
}

func getUserInput(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return input
}

func waitForKeyPress() {
	fmt.Println()
	fmt.Println("Press any key to exit...")
	buf := make([]byte, 1)
	os.Stdin.Read(buf) //nolint:errcheck
}
