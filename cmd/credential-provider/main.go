// Package main is the entrypoint for the credential provider.
package main

import (
	"github.com/cri-o/credential-provider/internal/pkg/app"
	"github.com/cri-o/credential-provider/internal/pkg/logger"
)

func main() {
	if err := app.Run(); err != nil {
		logger.L().Fatalf("Failed to run credential provider: %v", err)
	}
}
