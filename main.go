// RestoreSafe - Main Application Code

package main

import (
	"RestoreSafe/internal/archiver"
	"RestoreSafe/internal/mailer"
	"RestoreSafe/internal/utils"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// Embedding the 7-Zip executables for macOS and Windows into the binary.

//go:embed assets/7zz
var sevenZipMac []byte

//go:embed assets/7za.exe
var sevenZipWin []byte

func main() {
	// Load configuration
	configFile := flag.String("config", "config.json", "Path to the configuration file")
	flag.Parse()
	config, err := utils.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Prepare logger
	log.Printf("BackupDir: %s", config.BackupDir)
	logFilePath := filepath.Join(config.BackupDir, config.LogFileName)
	logger := utils.SetupLogger(logFilePath)
	defer logger.Close()

	if err := utils.CleanupLogFile(logFilePath, 1000, 5); err != nil {
		// Handle error
	}

	// Define command-line flags
	debug := flag.Bool("debug", false, "Enable debug mode for detailed console output")
	flag.Parse()

	if config.DebugMode {
		fmt.Println("Running in debug mode...")
	}

	// Get embedded 7-Zip executables
	zipBinaryPath, err := utils.LoadEmbeddedBinary(sevenZipMac, sevenZipWin)
	if err != nil {
		log.Fatalf("Failed to load embedded 7-Zip binary: %v", err)
	}

	log.Println("Backup process starting...")

	randomID, err := utils.GenerateRandomID(6)
	if err != nil {
		log.Fatalf("Failed to generate random ID: %v", err)
	}

	archiveFileName := fmt.Sprintf("backup_%s_%s.7z", time.Now().Format("2006-01-02"), randomID)
	tempArchivePath := filepath.Join(config.TempDir, archiveFileName)
	finalArchivePath := filepath.Join(config.BackupDir, archiveFileName)

	samePath, err := utils.CompareDirectoryPaths(config.TempDir, config.BackupDir)
	if err != nil {
		log.Fatalf("Failed to compare directory paths: %v", err)
	}
	if samePath {
		log.Printf("Creating archive at backup directory: %s", finalArchivePath)
		if err := archiver.CreateBackupArchive(config.Directories, finalArchivePath, *debug, zipBinaryPath, config); err != nil {
			log.Fatalf("Failed to create backup archive: %v", err)
		}
	} else {
		log.Printf("Creating archive at temp directory: %s", tempArchivePath)
		if err := archiver.CreateBackupArchive(config.Directories, tempArchivePath, *debug, zipBinaryPath, config); err != nil {
			log.Fatalf("Failed to create backup archive: %v", err)
		}

		log.Printf("Moving archive to backup directory: %s", finalArchivePath)
		if err := archiver.MoveArchive(tempArchivePath, finalArchivePath); err != nil {
			log.Fatalf("Failed to move archive: %v", err)
		}

		if err := utils.CleanupTempDir(config); err != nil {
			log.Printf("Failed to clean up temp directory: %v", err)
		}
	}

	if err := utils.CleanupBackupDir(config); err != nil {
		log.Printf("Failed to clean up backup directory: %v", err)
	}

	if err := mailer.SendStatusEmail(logFilePath, config); err != nil {
		log.Printf("Failed to send status email: %v", err)
	}

	log.Println("Backup process completed successfully!")

	if *debug {
		fmt.Println("Press Enter to exit.")
		fmt.Scanln() // Wait for user input before exiting
	}
}
