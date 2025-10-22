// Package k8s contains Kubernetes related logic.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"

	"github.com/cri-o/crio-credential-provider/internal/pkg/logger"
)

const k8sClaimKey = "kubernetes.io"

var (
	errRequestEmpty       = errors.New("request is empty")
	errTokenEmpty         = errors.New("request service account token is empty")
	errNoNamespaceInClaim = errors.New("no namespace found in kubernetes claim")
	errNamespaceNotString = errors.New("namespace is not a string object")
	errNoK8sClaimMap      = errors.New("kubernetes.io claim does not contain a map")
)

// ExtractNamespace extracts the namespace from the provided credential provider request.
func ExtractNamespace(req *cpv1.CredentialProviderRequest) (string, error) {
	if req == nil {
		return "", errRequestEmpty
	}

	if req.ServiceAccountToken == "" {
		return "", errTokenEmpty
	}

	// Use a reusable parser to avoid allocations
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())

	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(req.ServiceAccountToken, claims); err != nil {
		return "", fmt.Errorf("unable to parse JWT token: %w", err)
	}

	k8sClaim, ok := claims[k8sClaimKey]
	if !ok {
		return "", fmt.Errorf("no %s claim name in JWT claims found", k8sClaimKey)
	}

	k8sClaimMap, ok := k8sClaim.(map[string]any)
	if !ok {
		return "", errNoK8sClaimMap
	}

	namespaceAny, ok := k8sClaimMap["namespace"]
	if !ok {
		return "", errNoNamespaceInClaim
	}

	namespace, ok := namespaceAny.(string)
	if !ok {
		return "", errNamespaceNotString
	}

	return namespace, nil
}

// ClientFunc is the function for retrieving the Kubernetes client.
type ClientFunc func(token string) (kubernetes.Interface, error)

// RetrieveSecrets collects all secrets from the localhost node using the Kubernetes API.
func RetrieveSecrets(ctx context.Context, clientFunc ClientFunc, token, namespace string) (*corev1.SecretList, error) {
	client, err := clientFunc(token)
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

// APIServerHost can be used to retrieve the API server host:port combination
// from either /etc/kubernetes/apiserver-url.env or falling back to the default
// localhost:6443 one.
func APIServerHost(rootDir string) string {
	const (
		defaultHost             = "localhost:6443"
		defaultAPIServerEnvFile = "apiserver-url.env"
	)

	if !filepath.IsAbs(rootDir) {
		logger.L().Printf("Provided API server config dir %q is not an absolute path", rootDir)

		return defaultHost
	}

	envFilePath := filepath.Join(rootDir, defaultAPIServerEnvFile)

	if err := godotenv.Load(envFilePath); os.IsNotExist(err) {
		logger.L().Printf("Unable to find env file %q, using default API server host: %s", envFilePath, defaultHost)

		return defaultHost
	}

	// Avoid fmt.Sprintf allocation for simple string concatenation
	serviceHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	servicePort := os.Getenv("KUBERNETES_SERVICE_PORT")
	host := serviceHost + ":" + servicePort
	logger.L().Printf("Using API server host: %s", host)

	return host
}
