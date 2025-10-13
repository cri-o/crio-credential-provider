// Package main is the entrypoint for the credential provider.
package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cri-o/crio-credential-provider/internal/pkg/app"
	"github.com/cri-o/crio-credential-provider/internal/pkg/k8s"
	"github.com/cri-o/crio-credential-provider/internal/pkg/logger"
	"github.com/cri-o/crio-credential-provider/internal/pkg/version"
	"github.com/cri-o/crio-credential-provider/pkg/config"
)

func main() {
	showVersion := flag.Bool("version", false, "Display version information")
	showVersionJSON := flag.Bool("version-json", false, "Display version information as JSON")
	flag.Parse()

	if *showVersion {
		printVersion(false)

		return
	}

	if *showVersionJSON {
		printVersion(true)

		return
	}

	if err := app.Run(
		os.Stdin,
		config.RegistriesConfPath,
		config.AuthDir,
		config.KubeletAuthFilePath,
		func(token string) (kubernetes.Interface, error) {
			return kubernetes.NewForConfig(&rest.Config{
				Host:            k8s.APIServerHost(),
				BearerToken:     token,
				TLSClientConfig: rest.TLSClientConfig{Insecure: true},
			})
		},
	); err != nil {
		logger.L().Fatalf("Failed to run credential provider: %v", err)
	}
}

func printVersion(asJSON bool) {
	v, err := version.Get()
	if err != nil {
		logger.L().Fatalf("Failed to retrieve version: %v", err)
	}

	if asJSON {
		jsonString, err := v.JSONString()
		if err != nil {
			logger.L().Fatalf("Failed to get JSON string from version: %v", err)
		}

		fmt.Print(jsonString)

		return
	}

	fmt.Print(v)
}
