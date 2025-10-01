// Package auth contains everything related to auth file handling.
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/cri-o/credential-provider/internal/pkg/docker"
	"github.com/cri-o/credential-provider/internal/pkg/logger"
)

// CreateAuthFile can be used to create a auth file to /etc/crio/auth which follows the convention for CRI-O consumption.
func CreateAuthFile(secrets *corev1.SecretList, globalAuthFilePath, authDir, namespace, image string, mirrors []string) (string, error) {
	if namespace == "" {
		return "", errors.New("namespace is empty")
	}

	if secrets == nil {
		return "", errors.New("secrets is nil")
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
	auths := make(map[string]docker.ConfigEntry)

	for _, secret := range secrets.Items {
		logger.L().Printf("Parsing secret: %s", secret.Name)

		dockerConfigJSON, err := validDockerConfigSecret(secret)
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
			for _, m := range mirrors {
				logger.L().Printf("Checking if mirror %q matches registry %q", m, trimmedRegistry)

				if strings.HasPrefix(m, trimmedRegistry) {
					logger.L().Printf("Using mirror auth %q for registry from secret %q", m, trimmedRegistry)
					auths[trimmedRegistry] = auth
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
		encoded := base64.StdEncoding.EncodeToString([]byte(e.Username + ":" + e.Password))
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
	trimmed := strings.TrimPrefix(reg, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")

	return trimmed
}

func writeAuthFile(dir, image, namespace string, fileContents docker.ConfigJSON) (string, error) {
	if len(fileContents.Auths) == 0 {
		return "", errors.New("no auths found in file contents")
	}

	bytes, err := json.MarshalIndent(fileContents, "", "\t")
	if err != nil {
		return "", fmt.Errorf("marshal auth file: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ensure auth dir %q: %w", dir, err)
	}

	hash := sha256.Sum256([]byte(image))
	path := filepath.Join(dir, fmt.Sprintf("%s-%x.json", namespace, hash))

	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return "", fmt.Errorf("write auth file: %w", err)
	}

	return path, nil
}
