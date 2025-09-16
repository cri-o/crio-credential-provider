// Package main is the entrypoint for the credential provider.
package main

import (
	"github.com/cri-o/credential-provider/internal/pkg/app"
)

func main() {
	if err := app.Run(); err != nil {
		panic(err)
	}
}
