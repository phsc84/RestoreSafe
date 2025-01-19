package archiver

import (
	"RestoreSafe/internal/utils"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// extract7zip extracts the appropriate 7-Zip binary for the current OS to a temporary location.
func Extract7zip(filename string, embeddedBinary []byte) (string, error) {
	tempDir, err := os.MkdirTemp("", "zipExtractDir")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory for 7-Zip binary: %w", err)
	}

	outputPath := filepath.Join(tempDir, filename)
	if err := os.WriteFile(outputPath, embeddedBinary, 0755); err != nil {
		return "", fmt.Errorf("failed to write 7-Zip binary: %w", err)
	}

	// Remove macOS quarantine attribute if applicable
	if runtime.GOOS == "darwin" {
		err := exec.Command("xattr", "-d", "com.apple.quarantine", outputPath).Run()
		if err != nil {
			return "", fmt.Errorf("failed to remove macOS quarantine attribute: %w", err)
		}
	}

	return outputPath, nil
}

func CreateBackupArchive(directories []string, outputPath string, debug bool, zipBinaryPath string, config *utils.Config) error {
	defer os.RemoveAll(zipBinaryPath)

	// Open the log file
	logFilePath := filepath.Join(config.BackupDir, "backup.log")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	// Write to both console and log file
	logWriter := io.MultiWriter(os.Stdout, logFile)
	if !debug {
		// Only log to file in non-debug mode
		logWriter = logFile
	}

	// Log helper function
	log := func(format string, args ...interface{}) {
		fmt.Fprintf(logWriter, "[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}

	// a        = add
	// -mx=0    = level of compression => 0 = no compression / copy
	// -mhe=on  = enables archive header encryption
	// -mtm=on  = store modified timestamps
	// -mtc=on  = store created timestamps
	// -mta=on  = store accessed timestamps
	// -mtr=on  = store file attributes
	args := append([]string{"a", "-mx=0", "-mhe=on", "-mtm=on", "-mtc=on", "-mta=on", "-mtr=on", "-p" + config.Password, outputPath}, directories...)
	cmd := exec.Command(zipBinaryPath, args...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	log("Starting backup: %s", outputPath)
	if err := cmd.Run(); err != nil {
		log("7-Zip execution failed: %v", err)
		return fmt.Errorf("7-Zip failed: %w", err)
	}

	return nil
}

func MoveArchive(tempArchivePath string, finalArchivePath string) error {
	return os.Rename(tempArchivePath, finalArchivePath)
}
