package utils

import (
	"io"
	"log"
	"os"
)

func SetupLogger(logFile string) *os.File {
	log.Printf("Logging to file: %s", logFile)

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	log.SetOutput(io.MultiWriter(os.Stdout, file))
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return file
}
