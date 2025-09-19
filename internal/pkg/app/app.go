// Package app contains the main logic of the application.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.podman.io/image/v5/docker/reference"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"

	"github.com/cri-o/credential-provider/internal/pkg/auth"
	"github.com/cri-o/credential-provider/internal/pkg/config"
	"github.com/cri-o/credential-provider/internal/pkg/k8s"
	"github.com/cri-o/credential-provider/internal/pkg/logger"
	"github.com/cri-o/credential-provider/internal/pkg/mirrors"
)

// Run is the main entry point for the whole credential provider application.
func Run() error {
	logger.L().Print("Running credential provider")

	if _, err := os.Stat(config.RegistriesConfPath); err != nil {
		if os.IsNotExist(err) {
			logger.L().Printf("Registries conf path %q does not exist, stopping", config.RegistriesConfPath)

			return response()
		}

		return fmt.Errorf("unable to access registries conf path %q: %w", config.RegistriesConfPath, err)
	}

	logger.L().Print("Reading from stdin")

	stdinBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("unable to get logger: %w", err)
	}

	logger.L().Print("Got stdin, parsing JSON as CredentialProviderRequest")

	req := &cpv1.CredentialProviderRequest{}
	if err := json.Unmarshal(stdinBytes, req); err != nil {
		return fmt.Errorf("unable to parse JSON: %w", err)
	}

	logger.L().Printf("Parsed credential provider request for image %q", req.Image)

	image, err := reference.ParseDockerRef(req.Image)
	if err != nil {
		return fmt.Errorf("parse image name: %w", err)
	}

	if req.Image != image.String() {
		logger.L().Printf("Normalized provided image name from %q to %q", req.Image, image)
	}

	logger.L().Print("Parsing namespace from request")

	namespace, err := k8s.ExtractNamespace(req)
	if err != nil {
		return fmt.Errorf("unable to extract namespace: %w", err)
	}

	if err := json.Unmarshal(stdinBytes, &req); err != nil {
		return fmt.Errorf("unable to extract namespace from request: %w", err)
	}

	logger.L().Printf("Matching mirrors for registry config: %s", config.RegistriesConfPath)

	mirrors, err := mirrors.Match(req, config.RegistriesConfPath)
	if err != nil {
		return fmt.Errorf("unable to match mirrors: %w", err)
	}

	if len(mirrors) == 0 {
		logger.L().Printf("No mirrors found, will not write any auth file")

		return response()
	}

	logger.L().Printf("Got mirror(s) for %q: %q", image, strings.Join(mirrors, ", "))

	logger.L().Printf("Getting secrets from namespace: %s", namespace)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	secrets, err := k8s.RetrieveSecrets(ctx, req.ServiceAccountToken, namespace)
	if err != nil {
		return fmt.Errorf("unable to get secrets: %w", err)
	}

	logger.L().Printf("Got %d secret(s)", len(secrets.Items))

	authFilePath, err := auth.CreateAuthFile(secrets, config.KubeletAuthFilePath, config.AuthDir, namespace, image.String(), mirrors)
	if err != nil {
		return fmt.Errorf("unable to create auth file: %w", err)
	}

	logger.L().Printf("Auth file path: %s", authFilePath)

	return response()
}

func response() error {
	// Provide an empty response to the kubelet
	if err := json.NewEncoder(os.Stdout).Encode(cpv1.CredentialProviderResponse{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CredentialProviderResponse",
			APIVersion: "credentialprovider.kubelet.k8s.io/v1",
		},
		CacheKeyType: cpv1.RegistryPluginCacheKeyType,
	}); err != nil {
		return fmt.Errorf("unable to marshal credential provider response: %w", err)
	}

	return nil
}
