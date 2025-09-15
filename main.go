// credential-provier
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

var (
	registriesConfPath  = "/etc/containers/registries.conf"
	authDir             = "/etc/crio/auth"
	kubeletAuthFilePath = "/var/lib/kubelet/config.json"
)

type DockerConfigJSON struct {
	Auths map[string]DockerAuthConfig `json:"auths"`
}

type DockerAuthConfig struct {
	Auth string `json:"auth,omitempty"`
}

type DockerConfigEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func main() {
	l, err := newLogger()
	if err != nil {
		panic(err)
	}

	if err := run(l); err != nil {
		l.Fatalf("Fatal error: %v", err)
	}
}

func run(l *log.Logger) error {
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

	namespace, err := extractNamespace(req)
	if err != nil {
		return fmt.Errorf("unable to extract namespace: %w", err)
	}

	if err := json.Unmarshal(stdinBytes, &req); err != nil {
		return fmt.Errorf("unable to extract namespace from request: %w", err)
	}

	l.Printf("Getting secrets from namespace: %s", namespace)

	secrets, err := retrieveSecrets(ctx, req.ServiceAccountToken, namespace)
	if err != nil {
		return fmt.Errorf("unable to get secrets: %w", err)
	}

	l.Printf("Got %d secret(s)", len(secrets.Items))

	l.Printf("Matching mirrors for registry config: %s", registriesConfPath)

	mirrors, err := matchMirrors(req, registriesConfPath)
	if err != nil {
		return fmt.Errorf("unable to match mirrors: %w", err)
	}

	l.Printf("Got mirror(s) for %q: %q", req.Image, strings.Join(mirrors, ", "))

	authFilePath, err := createAuthFile(l, secrets, kubeletAuthFilePath, authDir, namespace, req.Image, mirrors)
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

func newLogger() (*log.Logger, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("unable to get executable name: %w", err)
	}

	fileName := filepath.Join(filepath.Dir(executable), "..", "logs")

	file, err := os.Create(fileName)
	if err != nil {
		return nil, fmt.Errorf("unable to open log file: %w", err)
	}

	writer := io.MultiWriter(file, os.Stderr)

	return log.New(writer, "", log.Ldate|log.Ltime|log.Lshortfile), nil
}

func extractNamespace(req *cpv1.CredentialProviderRequest) (string, error) {
	if req.ServiceAccountToken == "" {
		return "", errors.New("request service account token is empty")
	}

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(req.ServiceAccountToken, claims); err != nil {
		return "", fmt.Errorf("unable to parse JWT token: %w", err)
	}

	const claimName = "kubernetes.io"

	k8sClaim, ok := claims[claimName]
	if !ok {
		return "", fmt.Errorf("no %s claim name in JWT claims found", claimName)
	}

	k8sClaimMap, ok := k8sClaim.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%s claim does not contain a map", claimName)
	}

	namespaceAny, ok := k8sClaimMap["namespace"]
	if !ok {
		return "", errors.New("no namespace found in kubernetes claim")
	}

	namespace, ok := namespaceAny.(string)
	if !ok {
		return "", errors.New("namespace is not a string object")
	}

	return namespace, nil
}

func retrieveSecrets(ctx context.Context, token, namespace string) (*corev1.SecretList, error) {
	client, err := kubernetes.NewForConfig(&rest.Config{
		Host:            "localhost:6443",
		BearerToken:     token,
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to connect to Kubernetes API: %w", err)
	}

	secrets, err := client.CoreV1().
		Secrets(namespace).
		List(ctx, metav1.ListOptions{FieldSelector: "type=" + string(corev1.SecretTypeDockerConfigJson)})
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve secrets: %w", err)
	}

	return secrets, nil
}

func matchMirrors(req *cpv1.CredentialProviderRequest, registriesConfPath string) ([]string, error) {
	if req == nil || req.Image == "" {
		return nil, errors.New("request is nil or image is empty")
	}

	ctx := &types.SystemContext{SystemRegistriesConfPath: registriesConfPath}

	registry, err := sysregistriesv2.FindRegistry(ctx, req.Image)
	if err != nil {
		return nil, fmt.Errorf("loading registries configuration: %w", err)
	}

	if registry == nil {
		log.Printf("No registry found for image %q", req.Image)

		return nil, nil
	}

	// req.Image should include the explicit hostname
	sources := make([]string, len(registry.Mirrors))

	for i, mirror := range registry.Mirrors {
		sources[i] = mirror.Location
	}

	return sources, nil
}

func createAuthFile(l *log.Logger, secrets *corev1.SecretList, globalAuthFilePath, authDir, namespace, image string, mirrors []string) (string, error) {
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

	authfileContents, err := updateAuthContents(l, secrets, globalAuthContents, image, mirrors)
	if err != nil {
		return "", fmt.Errorf("unable to get namespace auth contents: %w", err)
	}

	// Write the namespace auth file to the auth directory /etc/crio/<namespace>-auth.json
	path, err := writeAuthFile(authDir, namespace, authfileContents)
	if err != nil {
		return "", fmt.Errorf("unable to write namespace auth file: %w", err)
	}

	l.Printf("Wrote auth file to %s with %d auth entrie(s)", path, len(authfileContents.Auths))

	return path, nil
}

func readGlobalAuthFile(path string) (DockerConfigJSON, error) {
	var fileContents DockerConfigJSON

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fileContents.Auths = map[string]DockerAuthConfig{}

			log.Printf("Global auth file does not exist, creating empty auths")

			return fileContents, nil
		}

		return DockerConfigJSON{}, fmt.Errorf("unable to read global auth file: %w", err)
	}

	if err = json.Unmarshal(raw, &fileContents.Auths); err != nil {
		return DockerConfigJSON{}, fmt.Errorf("unmarshaling JSON at %q: %w", path, err)
	}

	return fileContents, nil
}

func updateAuthContents(l *log.Logger, secrets *corev1.SecretList, globalAuthContents DockerConfigJSON, image string, mirrors []string) (DockerConfigJSON, error) {
	// Collect all matching auths keyed by registry or mirror
	auths := make(map[string]DockerConfigEntry)

	for _, secret := range secrets.Items {
		l.Printf("Parsing secret: %s", secret.Name)

		dockerConfigJSON, err := validDockerConfigSecret(secret)
		if err != nil {
			l.Printf("Skipping secret %q: %v", secret.Name, err)

			continue
		}

		for registry, authConfig := range dockerConfigJSON.Auths {
			l.Printf("Found docker config JSON auth in secret %q for %q", secret.Name, registry)

			auth, err := decodeDockerAuth(authConfig)
			if err != nil {
				l.Printf("Skipping secret %q because the docker config JSON auth is not parsable: %v", secret.Name, err)

				continue
			}

			trimmedRegistry := normalizeSecretRegistry(registry)
			for _, m := range mirrors {
				l.Printf("Checking if mirror %q matches registry %q", m, trimmedRegistry)

				if strings.HasPrefix(trimmedRegistry, m) {
					l.Printf("Using mirror auth %q for registry from secret %q", m, trimmedRegistry)
					auths[m] = auth
				}
			}

			if strings.HasPrefix(image, trimmedRegistry) {
				l.Printf("Using auth for registry %q matching image %q", trimmedRegistry, image)
				auths[trimmedRegistry] = auth
			}
		}
	}

	if len(auths) == 0 {
		l.Print("No docker auth found for any available secret")

		return DockerConfigJSON{}, errors.New("no docker auth found for any available secret")
	}

	// Merge global auth file contents with auths from secrets
	// auths take precedence over global auths
	fileContents := globalAuthContents
	if fileContents.Auths == nil {
		fileContents.Auths = map[string]DockerAuthConfig{}
	}

	for k, e := range auths {
		encoded := base64.StdEncoding.EncodeToString([]byte(e.Username + ":" + e.Password))
		fileContents.Auths[k] = DockerAuthConfig{Auth: encoded}
	}

	return fileContents, nil
}

func validDockerConfigSecret(secret corev1.Secret) (DockerConfigJSON, error) {
	dockerConfigJSON := DockerConfigJSON{}

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
func decodeDockerAuth(conf DockerAuthConfig) (DockerConfigEntry, error) {
	decoded, err := base64.StdEncoding.DecodeString(conf.Auth)
	if err != nil {
		return DockerConfigEntry{}, fmt.Errorf("unable to decode docker auth: %w", err)
	}

	user, passwordPart, valid := strings.Cut(string(decoded), ":")
	if !valid {
		return DockerConfigEntry{}, nil
	}

	password := strings.Trim(passwordPart, "\x00")

	return DockerConfigEntry{
		Username: user,
		Password: password,
	}, nil
}

func normalizeSecretRegistry(reg string) string {
	trimmed := strings.TrimPrefix(reg, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")

	return trimmed
}

func writeAuthFile(dir, namespace string, fileContents DockerConfigJSON) (string, error) {
	if fileContents.Auths == nil {
		return "", errors.New("no auths found in file contents")
	}

	bytes, err := json.MarshalIndent(fileContents, "", "\t")
	if err != nil {
		return "", fmt.Errorf("marshal auth file: %w", err)
	}

	path := filepath.Join(dir, namespace+".json")
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return "", fmt.Errorf("write auth file: %w", err)
	}

	return path, nil
}
