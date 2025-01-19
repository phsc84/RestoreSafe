package mailer

import (
	"RestoreSafe/internal/utils"
	"bufio"
	"fmt"
	"os"
	"strings"

	// Use: go get gopkg.in/gomail.v2
	"gopkg.in/gomail.v2"
)

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

func SendStatusEmail(logFile string, config *utils.Config) error {
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
	msg.Attach(logFile)

	dlr := gomail.NewDialer(config.EmailSMTPServer, config.EmailSMTPPort, config.EmailSMTPUser, config.EmailSMTPPassword)

	if err := dlr.DialAndSend(msg); err != nil {
		return err
	}
	return nil
}
