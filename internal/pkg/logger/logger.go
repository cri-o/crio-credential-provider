// Package logger contains the main logging logic.
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

	writer := io.MultiWriter(file, os.Stderr)

	return log.New(writer, "", log.Ldate|log.Ltime|log.Lshortfile), nil
}
