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

	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/types"
	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
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

var (
	registriesConfPath = "/etc/containers/registries.conf"
	tempAuthPath       = filepath.FromSlash(os.TempDir() + "/" + "%s-auth.json")
)

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

	authFilePath, err := createAuthFile(l, secrets, namespace, req.Image, mirrors)
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

	// req.Image should include the explicit hostname
	var sources []string

	registry, err := sysregistriesv2.FindRegistry(ctx, req.Image)
	if err != nil {
		return nil, fmt.Errorf("loading registries configuration: %w", err)
	}

	if registry == nil {
		log.Printf("No registry found for image %q", req.Image)
		return nil, nil
	}

	for _, mirror := range registry.Mirrors {
		sources = append(sources, mirror.Location)
	}

	return sources, nil
}

func createAuthFile(l *log.Logger, secrets *corev1.SecretList, namespace, image string, mirrors []string) (string, error) {
	if namespace == "" {
		return "", errors.New("namespace is empty")
	}
	if secrets == nil {
		return "", errors.New("secrets is nil")
	}

	// Collect all matching auths keyed by registry or mirror
	auths := make(map[string]DockerConfigEntry)

	for _, secret := range secrets.Items {
		if secret.Type != corev1.SecretTypeDockerConfigJson {
			continue
		}

		l.Printf("Parsing secret: %s", secret.Name)

		dockerConfigJSONBytes, ok := secret.Data[corev1.DockerConfigJsonKey]
		if !ok {
			l.Printf("Skipping secret %q because it does not contain data key %q", secret.Name, corev1.DockerConfigJsonKey)

			continue
		}

		dockerConfigJSON := DockerConfigJSON{}
		if err := json.Unmarshal(dockerConfigJSONBytes, &dockerConfigJSON); err != nil {
			l.Printf("Skipping secret %q because the docker config JSON is not parsable: %v", secret.Name, err)

			continue
		}

		for registry, authConfig := range dockerConfigJSON.Auths {
			l.Printf("Found docker config JSON auth in secret %q for %q", secret.Name, registry)

			trimmedRegistry := strings.TrimPrefix(registry, "http://")
			trimmedRegistry = strings.TrimPrefix(trimmedRegistry, "https://")

			auth, err := decodeDockerAuth(authConfig)
			if err != nil {
				l.Printf("Skipping secret %q because the docker config JSON auth is not parsable: %v", secret.Name, err)

				continue
			}

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
		return "", errors.New("no docker auth found for any available secret")
	}

	// Build and write docker config JSON to /tmp/<namespace>-auth.json
	fileContents := DockerConfigJSON{Auths: map[string]DockerAuthConfig{}}
	for k, e := range auths {
		encoded := base64.StdEncoding.EncodeToString([]byte(e.Username + ":" + e.Password))
		fileContents.Auths[k] = DockerAuthConfig{Auth: encoded}
	}

	bytes, err := json.MarshalIndent(fileContents, "", "\t")
	if err != nil {
		return "", fmt.Errorf("marshal auth file: %w", err)
	}
	path := fmt.Sprintf(tempAuthPath, namespace)
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return "", fmt.Errorf("write auth file: %w", err)
	}
	l.Printf("Wrote auth file to %s with %d auth entrie(s)", path, len(fileContents.Auths))
	return path, nil
}

// decodeDockerAuth decodes the username and password from conf
func decodeDockerAuth(conf DockerAuthConfig) (DockerConfigEntry, error) {
	decoded, err := base64.StdEncoding.DecodeString(conf.Auth)
	if err != nil {
		return DockerConfigEntry{}, err
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
