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

// Version is set at build time via -ldflags
var Version = "dev"

func main() {
	// Set working directory to the location of the executable
	exePath, err := os.Executable()
	if err != nil {
		exitWithError("Error determining executable path", err)
	}
	exeDir := filepath.Dir(exePath)
	if err := os.Chdir(exeDir); err != nil {
		exitWithError("Error setting working directory", err)
	}

	cfg, err := util.Load(filepath.Join(exeDir, "config.yaml"))
	if err != nil {
		exitWithError("Error loading configuration", err)
	}

	// Interactive menu mode
	for {
		printMenu()
		choice := getUserInput("Select an option (1-3): ")

		switch strings.TrimSpace(choice) {
		case "1":
			if err := engine.RunBackup(cfg, exeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
				waitForKeyPress()
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
	fmt.Printf("RestoreSafe v%s\n", Version)
	fmt.Println("Secure backup application")
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
