// Package k8s contains Kubernetes related logic.
package k8s

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

const k8sClaimKey = "kubernetes.io"

// ExtractNamespace extracts the namespace from the provided credential provider request.
func ExtractNamespace(req *cpv1.CredentialProviderRequest) (string, error) {
	if req == nil {
		return "", errors.New("request is empty")
	}

	if req.ServiceAccountToken == "" {
		return "", errors.New("request service account token is empty")
	}

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(req.ServiceAccountToken, claims); err != nil {
		return "", fmt.Errorf("unable to parse JWT token: %w", err)
	}

	k8sClaim, ok := claims[k8sClaimKey]
	if !ok {
		return "", fmt.Errorf("no %s claim name in JWT claims found", k8sClaimKey)
	}

	k8sClaimMap, ok := k8sClaim.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%s claim does not contain a map", k8sClaimKey)
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

// RetrieveSecrets collects all secrets from the localhost node using the Kubernetes API.
func RetrieveSecrets(ctx context.Context, token, namespace string) (*corev1.SecretList, error) {
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
