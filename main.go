// RestoreSafe - Main Application Code

package main

import (
	"bufio"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	// Use: go get gopkg.in/gomail.v2
	"gopkg.in/gomail.v2"
)

// Configurations - Loaded from external JSON file
type Config struct {
	Directories          []string `json:"directories"`
	BackupDir            string   `json:"backup_dir"`
	TempDir              string   `json:"temp_dir"`
	Password             string   `json:"password"`
	RetainRecentBackups  int      `json:"retain_recent_backups"`
	LogFileName          string   `json:"log_file_name"`
	DebugMode            bool     `json:"debug_mode"`
	EmailRecipient       string   `json:"email_recipient"`
	EmailSender          string   `json:"email_sender"`
	EmailSMTPServer      string   `json:"email_smtp_server"`
	EmailSMTPPort        int      `json:"email_smtp_port"`
	EmailSMTPAuthEnabled bool     `json:"email_smtp_auth_enabled"`
	EmailSMTPUser        string   `json:"email_smtp_user"`
	EmailSMTPPassword    string   `json:"email_smtp_password"`
}

var config Config

func main() {
	// Define command-line flags
	debug := flag.Bool("debug", false, "Enable debug mode for detailed console output")
	flag.Parse()

	configFile := flag.String("config", "config.json", "Path to the configuration file")
	flag.Parse()

	// Load the configuration file
	if err := loadConfig(*configFile); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if config.DebugMode {
		fmt.Println("Running in debug mode...")
	}

	// Prepare logger
	logFile := filepath.Join(config.BackupDir, config.LogFileName)
	logger := setupLogger(logFile)
	defer logger.Close()

	log.Println("Backup process starting...")

	randomID, err := generateRandomID(6)
	if err != nil {
		log.Fatalf("Failed to generate random ID: %v", err)
	}

	archiveFileName := fmt.Sprintf("backup_%s_%s.7z", time.Now().Format("2006-01-02"), randomID)
	tempArchivePath := filepath.Join(config.TempDir, archiveFileName)
	finalArchivePath := filepath.Join(config.BackupDir, archiveFileName)

	samePath, err := compareDirectoryPaths(config.TempDir, config.BackupDir)
	if err != nil {
		log.Fatalf("Failed to compare directory paths: %v", err)
	}
	if samePath {
		log.Printf("Creating archive at backup directory: %s", finalArchivePath)
		if err := createBackupArchive(config.Directories, finalArchivePath, *debug); err != nil {
			log.Fatalf("Failed to create backup archive: %v", err)
		}
	} else {
		log.Printf("Creating archive at temp directory: %s", tempArchivePath)
		if err := createBackupArchive(config.Directories, tempArchivePath, *debug); err != nil {
			log.Fatalf("Failed to create backup archive: %v", err)
		}

		log.Printf("Moving archive to backup directory: %s", finalArchivePath)
		if err := moveArchive(tempArchivePath, finalArchivePath); err != nil {
			log.Fatalf("Failed to move archive: %v", err)
		}

		if err := cleanupTempDir(); err != nil {
			log.Printf("Failed to clean up temp directory: %v", err)
		}
	}

	if err := cleanupBackupDir(); err != nil {
		log.Printf("Failed to clean up backup directory: %v", err)
	}

	if err := sendStatusEmail(logFile); err != nil {
		log.Printf("Failed to send status email: %v", err)
	}

	log.Println("Backup process completed successfully!")

	if *debug {
		fmt.Println("Press Enter to exit.")
		fmt.Scanln() // Wait for user input before exiting
	}
}

// Embedding the 7-Zip executables for macOS and Windows into the binary.

//go:embed assets/7zz
var sevenZipMac []byte

//go:embed assets/7za.exe
var sevenZipWin []byte

// extract7zz extracts the appropriate 7-Zip binary for the current OS to a temporary location.
func extract7zz(tempDir string) (string, error) {
	var embeddedData []byte
	var fileName string

	switch runtime.GOOS {
	case "darwin":
		embeddedData = sevenZipMac
		fileName = "7zz"
	case "windows":
		embeddedData = sevenZipWin
		fileName = "7za.exe"
	default:
		return "", errors.New("unsupported operating system")
	}

	outputPath := filepath.Join(tempDir, fileName)
	if err := os.WriteFile(outputPath, embeddedData, 0755); err != nil {
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

func loadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &config)
}

func setupLogger(logFile string) *os.File {
	log.Printf("Logging to file: %s", logFile)

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	log.SetOutput(io.MultiWriter(os.Stdout, file))
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return file
}

func generateRandomID(length int) (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}
	return string(result), nil
}

func compareDirectoryPaths(path1, path2 string) (bool, error) {
	if len(strings.TrimSpace(path1)) == 0 {
		log.Printf("Temp directory not set, using backup directory.")
		return true, nil
	} else {
		// Clean and get absolute paths: handles relative paths, ".", "..", to ensure consistent comparison.
		absPath1, err := filepath.Abs(path1)
		if err != nil {
			return false, fmt.Errorf("error getting absolute path for %s: %w", path1, err)
		}
		absPath2, err := filepath.Abs(path2)
		if err != nil {
			return false, fmt.Errorf("error getting absolute path for %s: %w", path2, err)
		}
		//EvalSymlinks to resolve symbolic links
		absPath1, err = filepath.EvalSymlinks(absPath1)
		if err != nil {
			return false, fmt.Errorf("error resolving symlinks for %s: %w", path1, err)
		}
		absPath2, err = filepath.EvalSymlinks(absPath2)
		if err != nil {
			return false, fmt.Errorf("error resolving symlinks for %s: %w", path2, err)
		}
		return absPath1 == absPath2, nil
	}
}

func sortFilesByModTime(files []os.FileInfo) []os.FileInfo {
	sort.Slice(
		files, func(i, j int) bool {
			return files[i].ModTime().Before(files[j].ModTime())
		},
	)
	return files
}

func createBackupArchive(directories []string, outputPath string, debug bool) error {
	zipBinary, err := extract7zz(config.TempDir)
	if err != nil {
		log.Fatalf("Failed to extract 7-Zip binary: %v", err)
	}
	defer os.Remove(zipBinary) // Clean up after execution

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
	cmd := exec.Command(zipBinary, args...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	log("Starting backup: %s", outputPath)
	if err := cmd.Run(); err != nil {
		log("7-Zip execution failed: %v", err)
		return fmt.Errorf("7-Zip failed: %w", err)
	}

	return nil
}

func moveArchive(tempArchivePath string, finalArchivePath string) error {
	return os.Rename(tempArchivePath, finalArchivePath)
}

func cleanupTempDir() error {
	files, err := os.ReadDir(config.TempDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), "backup_") && strings.HasSuffix(file.Name(), ".7z") {
			log.Printf("Removing old backup file: %s", file.Name())
			if err := os.Remove(filepath.Join(config.TempDir, file.Name())); err != nil {
				log.Printf("Failed to delete old backup file: %v", err)
			}
		}
	}

	return nil
}

func cleanupBackupDir() error {
	files, err := os.ReadDir(config.BackupDir)
	if err != nil {
		return err
	}

	backupFiles := []os.FileInfo{}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "backup_") && strings.HasSuffix(file.Name(), ".7z") {
			fileInfo, err := file.Info()
			if err != nil {
				return err
			}
			backupFiles = append(backupFiles, fileInfo)
		}
	}

	if len(backupFiles) <= config.RetainRecentBackups {
		return nil
	}

	// Sort by creation time
	backupFiles = sortFilesByModTime(backupFiles)

	// Remove old backup files
	for _, oldFile := range backupFiles[:len(backupFiles)-config.RetainRecentBackups] {
		log.Printf("Removing old backup file: %s", oldFile.Name())
		if err := os.Remove(filepath.Join(config.BackupDir, oldFile.Name())); err != nil {
			log.Printf("Failed to delete old backup file: %v", err)
		}
	}

	return nil
}

func getLastLines(logFile string, numLines int) ([]string, error) {
	file, err := os.Open(logFile)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > numLines {
			lines = lines[1:] // Keep only the last numLines
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning file: %w", err)
	}

	return lines, nil
}

func sendStatusEmail(logFile string) error {
	// Get the last lines of the log file
	lastLines, err := getLastLines(logFile, 20)
	if err != nil {
		return err
	}
	msg := gomail.NewMessage()
	msg.SetHeader("From", config.EmailSender)
	msg.SetHeader("To", config.EmailRecipient)
	msg.SetHeader("Subject", "Backup Status")
	msg.SetBody("text/plain", strings.Join(lastLines, "\n"))

	dlr := gomail.NewDialer(config.EmailSMTPServer, config.EmailSMTPPort, config.EmailSMTPUser, config.EmailSMTPPassword)

	if err := dlr.DialAndSend(msg); err != nil {
		return err
	}
	return nil
}
