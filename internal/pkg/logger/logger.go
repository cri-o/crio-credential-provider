// Package logger contains the main logging logic.
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/journal"
)

// New createas a new default logger instance.
func New() (*log.Logger, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("unable to get executable name: %w", err)
	}

	fileName := filepath.Join(filepath.Dir(executable), "..", "logs")

	file, err := os.Create(fileName)
	if err != nil {
		return nil, fmt.Errorf("unable to open log file: %w", err)
	}

	writer := io.MultiWriter(file, os.Stderr, &journalWriter{})

	return log.New(writer, "", log.Ldate|log.Ltime|log.Lshortfile), nil
}

type journalWriter struct{}

func (*journalWriter) Write(p []byte) (int, error) {
	// log.Ldate + log.Ltime have a length of 20 including 2 spaces
	const trimLen = 20

	trimmed := string(p)
	if len(p) > trimLen {
		trimmed = trimmed[trimLen:]
	}

	if err := journal.Send(trimmed, journal.PriInfo, nil); err != nil {
		return 0, fmt.Errorf("unable to send to journald: %w", err)
	}

	return len(p), nil
}
