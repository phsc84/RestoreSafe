package utils

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func CleanupLogFile(logFilePath string, maxLines int, maxAgeDays int) error {
	file, err := os.Open(logFilePath)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning log file: %w", err)
	}

	var filteredLines []string
	if maxAgeDays > 0 {
		now := time.Now()
		var lastTimestamp time.Time
		var linesToKeep []string // Buffer for lines without timestamps

		timestampRegex := regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\]`) // Improved regex

		for _, line := range lines {
			match := timestampRegex.FindStringSubmatch(line)
			if len(match) > 1 { //Timestamp found
				timeStr := match[1]
				t, err := time.Parse("2006-01-02 15:04:05", timeStr)
				if err != nil {
					fmt.Printf("Error parsing time from log line: %s, error: %s\n", line, err)
				} else {
					if now.Sub(t).Hours() <= float64(maxAgeDays*24) {
						if len(linesToKeep) > 0 {
							filteredLines = append(filteredLines, linesToKeep...)
							linesToKeep = nil
						}
						filteredLines = append(filteredLines, line)
						lastTimestamp = t
					} else {
						// Timestamp is too old, clear the buffer
						linesToKeep = nil
					}
				}
			} else {
				if !lastTimestamp.IsZero() && now.Sub(lastTimestamp).Hours() > float64(maxAgeDays*24) {
					continue //Skip the line if the last timestamp is older than maxAgeDays
				} else {
					linesToKeep = append(linesToKeep, line)
				}
			}
		}
		//Add the remaining lines if the last timestamp is within the range
		if !lastTimestamp.IsZero() && now.Sub(lastTimestamp).Hours() <= float64(maxAgeDays*24) {
			filteredLines = append(filteredLines, linesToKeep...)
		}
	} else if maxLines > 0 && len(lines) > maxLines {
		filteredLines = lines[len(lines)-maxLines:]
	} else {
		return nil
	}

	file, err = os.Create(logFilePath)
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range filteredLines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return fmt.Errorf("writing to log file: %w", err)
		}
	}
	return writer.Flush()
}

func CleanupTempDir(config *Config) error {
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

func CleanupBackupDir(config *Config) error {
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
	backupFiles = SortFilesByModTime(backupFiles)

	// Remove old backup files
	for _, oldFile := range backupFiles[:len(backupFiles)-config.RetainRecentBackups] {
		log.Printf("Removing old backup file: %s", oldFile.Name())
		if err := os.Remove(filepath.Join(config.BackupDir, oldFile.Name())); err != nil {
			log.Printf("Failed to delete old backup file: %v", err)
		}
	}

	return nil
}
