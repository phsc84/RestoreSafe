package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// LoadConfig loads the configuration from the specified JSON file.
func LoadConfig(path string) (*Config, error) {
	// Open the config file.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	// Decode the JSON data.
	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config JSON: %w", err)
	}

	// Basic validation (optional but recommended):
	if config.BackupDir == "" {
		return nil, fmt.Errorf("backup_dir is required in config")
	}
	if config.Password == "" {
		return nil, fmt.Errorf("password is required in config")
	}

	return &config, nil // Return a pointer to the Config struct
}

func LoadEmbeddedBinary(sevenZipMac []byte, sevenZipWin []byte) (string, error) {
	var filename string
	var embeddedBinary []byte
	var err error

	switch runtime.GOOS {
	case "darwin":
		filename = "7zz"
		embeddedBinary = sevenZipMac
		err = nil
	case "windows":
		filename = "7za.exe"
		embeddedBinary = sevenZipWin
		err = nil
	default:
		return "", errors.New("unsupported operating system")
	}

	tempDir, err := os.MkdirTemp("", "binaryExtractDir")
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
