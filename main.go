package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

type DockerConfigJSON struct {
	Auths map[string]DockerConfigEntry `json:"auths"`
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
	secrets, err := retrieveSecrets(req.ServiceAccountToken, namespace)
	if err != nil {
		return fmt.Errorf("unable to get secrets: %w", err)
	}
	l.Printf("Got %d secret(s)", len(secrets.Items))

	var foundEntry *DockerConfigEntry
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
		err := json.Unmarshal(dockerConfigJSONBytes, &dockerConfigJSON)
		if err != nil {
			l.Printf("Skipping secret %q because the docker config JSON is not parsable: %v", secret.Name, err)

			continue
		}

		foundMatchingAuth := false
		for _, auth := range dockerConfigJSON.Auths {
			l.Printf("Found matching docker config JSON auth in secret: %s", secret.Name)
			// TODO: Resolve registries.conf

			foundEntry = &auth
			foundMatchingAuth = true

			break
		}

		if foundMatchingAuth {
			break
		}

		l.Printf("Found no matching docker config JSON auth in secret: %s", secret.Name)
	}

	if foundEntry != nil {
		response := cpv1.CredentialProviderResponse{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CredentialProviderResponse",
				APIVersion: "credentialprovider.kubelet.k8s.io/v1",
			},
			CacheKeyType:  cpv1.RegistryPluginCacheKeyType,
			CacheDuration: &metav1.Duration{Duration: 5 * time.Minute},
			Auth: map[string]cpv1.AuthConfig{
				req.Image: {Username: foundEntry.Username, Password: foundEntry.Password},
			},
		}

		// Provide the response to the kubelet
		if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
			return fmt.Errorf("unable to marshal credential provider response: %w", err)
		}

		l.Printf("Successfully provided credentials for image %q to the kubelet", req.Image)
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

func retrieveSecrets(token, namespace string) (*corev1.SecretList, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := &http.Client{Transport: transport}

	httpReq, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("https://localhost:6443/api/v1/namespaces/%s/secrets", namespace),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("unable create HTTP request: %w", err)
	}

	httpReq.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("unable to do HTTP request to obtain secrets: %w", err)
	}
	defer func() { resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}

	secrets := &corev1.SecretList{}
	if err := json.Unmarshal(body, &secrets); err != nil {
		return nil, fmt.Errorf("unable to unmarshal secret list: %w", err)
	}

	return secrets, nil
}
