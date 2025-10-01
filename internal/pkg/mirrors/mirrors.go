// Package mirrors contains the mirror matching logic.
package mirrors

import (
	"errors"
	"fmt"

	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

// Match can be used to retrieve all mirrors for a registry configuration.
func Match(req *cpv1.CredentialProviderRequest, registriesConfPath string) ([]string, error) {
	if req == nil || req.Image == "" {
		return nil, errors.New("request is nil or image is empty")
	}

	ctx := &types.SystemContext{SystemRegistriesConfPath: registriesConfPath}

	registry, err := sysregistriesv2.FindRegistry(ctx, req.Image)
	if err != nil {
		return nil, fmt.Errorf("loading registries configuration: %w", err)
	}

	if registry == nil {
		return nil, nil
	}

	// req.Image should include the explicit hostname
	sources := make([]string, len(registry.Mirrors))

	for i, mirror := range registry.Mirrors {
		sources[i] = mirror.Location
	}

	return sources, nil
}
