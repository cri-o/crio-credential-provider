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
	l, err := logger.New()
	if err != nil {
		return fmt.Errorf("unable to create logger: %w", err)
	}

	l.Print("Running credential provider")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	l.Print("Reading from stdin")

	stdinBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("unable to get logger: %w", err)
	}

	l.Print("Got stdin, parsing JSON as CredentialProviderRequest")

	req := &cpv1.CredentialProviderRequest{}
	if err := json.Unmarshal(stdinBytes, req); err != nil {
		return fmt.Errorf("unable to parse JSON: %w", err)
	}

	l.Printf("Parsed credential provider request for image %q", req.Image)

	l.Print("Parsing namespace from request")

	namespace, err := k8s.ExtractNamespace(req)
	if err != nil {
		return fmt.Errorf("unable to extract namespace: %w", err)
	}

	if err := json.Unmarshal(stdinBytes, &req); err != nil {
		return fmt.Errorf("unable to extract namespace from request: %w", err)
	}

	l.Printf("Getting secrets from namespace: %s", namespace)

	secrets, err := k8s.RetrieveSecrets(ctx, req.ServiceAccountToken, namespace)
	if err != nil {
		return fmt.Errorf("unable to get secrets: %w", err)
	}

	l.Printf("Got %d secret(s)", len(secrets.Items))

	l.Printf("Matching mirrors for registry config: %s", config.RegistriesConfPath)

	mirrors, err := mirrors.Match(req, config.RegistriesConfPath)
	if err != nil {
		return fmt.Errorf("unable to match mirrors: %w", err)
	}

	l.Printf("Got mirror(s) for %q: %q", req.Image, strings.Join(mirrors, ", "))

	authFilePath, err := auth.CreateAuthFile(l, secrets, config.KubeletAuthFilePath, config.AuthDir, namespace, req.Image, mirrors)
	if err != nil {
		return fmt.Errorf("unable to create auth file: %w", err)
	}

	l.Printf("Auth file path: %s", authFilePath)

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
