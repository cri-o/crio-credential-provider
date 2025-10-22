// Package logger contains the main logging logic.
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/coreos/go-systemd/v22/journal"
)

var (
	instance *log.Logger
	once     sync.Once
)

// L can be used to get the default logging instance.
func L() *log.Logger {
	once.Do(func() { instance = newLogger() })

	return instance
}

// newLogger creates a new default logger instance.
func newLogger() *log.Logger {
	writer := io.MultiWriter(os.Stderr, &journalWriter{})

	return log.New(writer, "", log.Ldate|log.Ltime|log.Lshortfile)
}

type journalWriter struct{}

func (*journalWriter) Write(p []byte) (int, error) {
	// log.Ldate + log.Ltime have a length of 20 including 2 spaces
	const trimLen = 20

	// Avoid string allocation by using byte slicing directly
	var trimmed string
	if len(p) > trimLen {
		// Convert only the necessary portion to string
		trimmed = string(p[trimLen:])
	} else {
		trimmed = string(p)
	}

	if err := journal.Send(trimmed, journal.PriInfo, nil); err != nil {
		return 0, fmt.Errorf("unable to send to journald: %w", err)
	}

	return len(p), nil
}
