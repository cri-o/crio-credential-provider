// Package main is the entrypoint for the credential provider.
package main

import (
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cri-o/crio-credential-provider/internal/pkg/app"
	"github.com/cri-o/crio-credential-provider/internal/pkg/k8s"
	"github.com/cri-o/crio-credential-provider/internal/pkg/logger"
	"github.com/cri-o/crio-credential-provider/pkg/config"
)

func main() {
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
