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

	"github.com/cri-o/crio-credential-provider/internal/pkg/auth"
	"github.com/cri-o/crio-credential-provider/internal/pkg/k8s"
	"github.com/cri-o/crio-credential-provider/internal/pkg/logger"
	"github.com/cri-o/crio-credential-provider/internal/pkg/mirrors"
)

// Run is the main entry point for the whole credential provider application.
func Run(stdin io.Reader, registriesConfPath, authDir, kubeletAuthFilePath string, clientFunc k8s.ClientFunc) error {
	logger.L().Print("Running credential provider")

	if _, err := os.Stat(registriesConfPath); err != nil {
		if os.IsNotExist(err) {
			logger.L().Printf("Registries conf path %q does not exist, stopping", registriesConfPath)

			return response()
		}

		return fmt.Errorf("unable to access registries conf path %q: %w", registriesConfPath, err)
	}

	logger.L().Print("Reading from stdin")

	stdinBytes, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("unable to read credential provider request from stdin: %w", err)
	}

	logger.L().Print("Got stdin, parsing JSON as CredentialProviderRequest")

	req := &cpv1.CredentialProviderRequest{}
	if err := json.Unmarshal(stdinBytes, req); err != nil {
		return fmt.Errorf("unable to parse JSON: %w", err)
	}

	// req.Image does not contain the full image reference. It's a result of
	// `res, _ := reference.ParseNormalizedNamed()` where `res.Name()` get's passed down
	// to each credential provider. See:
	// https://github.com/kubernetes/kubernetes/blob/6070f5a/pkg/kubelet/images/image_manager.go#L192-L195
	// which calls into:
	// https://github.com/kubernetes/kubernetes/blob/6070f5a/pkg/util/parsers/parsers.go#L29-L37
	logger.L().Printf("Parsed credential provider request for image %q", req.Image)

	logger.L().Print("Parsing namespace from request")

	namespace, err := k8s.ExtractNamespace(req)
	if err != nil {
		return fmt.Errorf("unable to extract namespace: %w", err)
	}

	logger.L().Printf("Matching mirrors for registry config: %s", registriesConfPath)

	mirrors, err := mirrors.Match(req, registriesConfPath)
	if err != nil {
		return fmt.Errorf("unable to match mirrors: %w", err)
	}

	if len(mirrors) == 0 {
		logger.L().Printf("No mirrors found, will not write any auth file")

		return response()
	}

	logger.L().Printf("Got mirror(s) for %q: %q", req.Image, strings.Join(mirrors, ", "))

	logger.L().Printf("Getting secrets from namespace: %s", namespace)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	secrets, err := k8s.RetrieveSecrets(ctx, clientFunc, req.ServiceAccountToken, namespace)
	if err != nil {
		return fmt.Errorf("unable to get secrets: %w", err)
	}

	logger.L().Printf("Got %d secret(s)", len(secrets.Items))

	authFilePath, err := auth.CreateAuthFile(secrets, kubeletAuthFilePath, authDir, namespace, req.Image, mirrors)
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
