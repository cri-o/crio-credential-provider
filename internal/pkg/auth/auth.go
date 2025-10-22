// Package auth contains everything related to auth file handling.
package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/cri-o/crio-credential-provider/internal/pkg/docker"
	"github.com/cri-o/crio-credential-provider/internal/pkg/logger"
	"github.com/cri-o/crio-credential-provider/pkg/auth"
)

var (
	errNoAuths        = errors.New("no auths found in file contents")
	errNamespaceEmpty = errors.New("namespace is empty")
	errSecretsNil     = errors.New("secrets is nil")
)

// CreateAuthFile can be used to create a auth file to /etc/crio/auth which follows the convention for CRI-O consumption.
func CreateAuthFile(secrets *corev1.SecretList, globalAuthFilePath, authDir, namespace, image string, mirrors []string) (string, error) {
	if namespace == "" {
		return "", errNamespaceEmpty
	}

	if secrets == nil {
		return "", errSecretsNil
	}

	globalAuthContents, err := readGlobalAuthFile(globalAuthFilePath)
	if err != nil {
		return "", fmt.Errorf("unable to read global auth file: %w", err)
	}

	authfileContents := updateAuthContents(secrets, globalAuthContents, image, mirrors)

	// Write the namespace auth file to the auth directory /etc/crio/<namespace>-<image_name_sha256>.json
	path, err := writeAuthFile(authDir, image, namespace, authfileContents)
	if err != nil {
		return "", fmt.Errorf("unable to write namespace auth file: %w", err)
	}

	logger.L().Printf("Wrote auth file to %s with %d number of entries", path, len(authfileContents.Auths))

	return path, nil
}

func readGlobalAuthFile(path string) (docker.ConfigJSON, error) {
	var fileContents docker.ConfigJSON

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fileContents.Auths = map[string]docker.AuthConfig{}

			return fileContents, nil
		}

		return docker.ConfigJSON{}, fmt.Errorf("unable to read global auth file: %w", err)
	}

	if err = json.Unmarshal(raw, &fileContents); err != nil {
		return docker.ConfigJSON{}, fmt.Errorf("unmarshaling JSON at %q: %w", path, err)
	}

	return fileContents, nil
}

func updateAuthContents(secrets *corev1.SecretList, globalAuthContents docker.ConfigJSON, image string, mirrors []string) docker.ConfigJSON {
	// Collect all matching auths keyed by registry or mirror
	// Pre-allocate with estimated capacity to reduce reallocations
	estimatedCapacity := len(secrets.Items) * len(mirrors)
	if estimatedCapacity == 0 {
		estimatedCapacity = 8 // reasonable default
	}

	auths := make(map[string]docker.ConfigEntry, estimatedCapacity)

	// Optimize by avoiding range value copies for large structs
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		logger.L().Printf("Parsing secret: %s", secret.Name)

		dockerConfigJSON, err := validDockerConfigSecret(*secret)
		if err != nil {
			logger.L().Printf("Skipping secret %q: %v", secret.Name, err)

			continue
		}

		for registry, authConfig := range dockerConfigJSON.Auths {
			logger.L().Printf("Found docker config JSON auth in secret %q for %q", secret.Name, registry)

			auth, err := decodeDockerAuth(authConfig)
			if err != nil {
				logger.L().Printf("Skipping secret %q because the docker config JSON auth is not parsable: %v", secret.Name, err)

				continue
			}

			trimmedRegistry := normalizeSecretRegistry(registry)

			// Check mirrors with early exit optimization
			mirrorsLen := len(mirrors)
			for j := range mirrorsLen {
				m := mirrors[j]
				logger.L().Printf("Checking if mirror %q matches registry %q", m, trimmedRegistry)

				if strings.HasPrefix(m, trimmedRegistry) {
					logger.L().Printf("Using mirror auth %q for registry from secret %q", m, trimmedRegistry)
					auths[trimmedRegistry] = auth

					break // No need to check remaining mirrors once matched
				}
			}

			if strings.HasPrefix(image, trimmedRegistry) {
				logger.L().Printf("Using auth for registry %q matching image %q", trimmedRegistry, image)
				auths[trimmedRegistry] = auth
			}
		}
	}

	if len(auths) == 0 {
		logger.L().Print("No docker auth found for any available secret")
	}

	// Merge global auth file contents with auths from secrets
	// auths take precedence over global auths
	fileContents := globalAuthContents
	if fileContents.Auths == nil {
		fileContents.Auths = map[string]docker.AuthConfig{}
	}

	for k, e := range auths {
		// Pre-calculate the size to avoid string concatenation allocations
		credentials := make([]byte, 0, len(e.Username)+1+len(e.Password))
		credentials = append(credentials, e.Username...)
		credentials = append(credentials, ':')
		credentials = append(credentials, e.Password...)
		encoded := base64.StdEncoding.EncodeToString(credentials)
		fileContents.Auths[k] = docker.AuthConfig{Auth: encoded}
	}

	return fileContents
}

func validDockerConfigSecret(secret corev1.Secret) (docker.ConfigJSON, error) {
	dockerConfigJSON := docker.ConfigJSON{}

	if secret.Type != corev1.SecretTypeDockerConfigJson {
		return dockerConfigJSON, errors.New("secret is not a docker config JSON secret")
	}

	dockerConfigJSONBytes, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return dockerConfigJSON, fmt.Errorf("skipping secret %q because it does not contain data key %q", secret.Name, corev1.DockerConfigJsonKey)
	}

	if err := json.Unmarshal(dockerConfigJSONBytes, &dockerConfigJSON); err != nil {
		return dockerConfigJSON, fmt.Errorf("skipping secret %q because the docker config JSON is not parsable: %w", secret.Name, err)
	}

	return dockerConfigJSON, nil
}

// decodeDockerAuth decodes the username and password from conf.
func decodeDockerAuth(conf docker.AuthConfig) (docker.ConfigEntry, error) {
	decoded, err := base64.StdEncoding.DecodeString(conf.Auth)
	if err != nil {
		return docker.ConfigEntry{}, fmt.Errorf("unable to decode docker auth: %w", err)
	}

	user, passwordPart, valid := strings.Cut(string(decoded), ":")
	if !valid {
		return docker.ConfigEntry{}, nil
	}

	password := strings.Trim(passwordPart, "\x00")

	return docker.ConfigEntry{
		Username: user,
		Password: password,
	}, nil
}

func normalizeSecretRegistry(reg string) string {
	// Avoid double allocation by checking which prefix exists first
	if strings.HasPrefix(reg, "https://") {
		return reg[8:] // len("https://") == 8
	}

	if strings.HasPrefix(reg, "http://") {
		return reg[7:] // len("http://") == 7
	}

	return reg
}

func writeAuthFile(dir, image, namespace string, fileContents docker.ConfigJSON) (string, error) {
	if len(fileContents.Auths) == 0 {
		return "", errNoAuths
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ensure auth dir %q: %w", dir, err)
	}

	path, err := auth.FilePath(dir, namespace, image)
	if err != nil {
		return "", fmt.Errorf("get auth path: %w", err)
	}

	// Write directly to file using encoder to avoid intermediate buffer allocation
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("open auth file: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.L().Printf("Failed to close auth file: %v", closeErr)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\t")

	if err := encoder.Encode(fileContents); err != nil {
		return "", fmt.Errorf("encode auth file: %w", err)
	}

	return path, nil
}
