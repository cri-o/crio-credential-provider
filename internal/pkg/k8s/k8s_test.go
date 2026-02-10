package k8s

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

var (
	// Cache the ECDSA key to avoid expensive key generation for every test.
	testECDSAKey     *ecdsa.PrivateKey
	testECDSAKeyOnce sync.Once
)

func getTestECDSAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	testECDSAKeyOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		testECDSAKey = key
	})

	return testECDSAKey
}

func TestExtractNamespace(t *testing.T) {
	t.Parallel()

	prepareToken := func(claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		key := getTestECDSAKey(t)

		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		return tokenString
	}

	for name, tc := range map[string]struct {
		req               *cpv1.CredentialProviderRequest
		shouldErr         bool
		expectedNamespace string
	}{
		"success": {
			req: &cpv1.CredentialProviderRequest{
				ServiceAccountToken: prepareToken(jwt.MapClaims{
					k8sClaimKey: map[string]any{
						"namespace": "default",
					},
				}),
			},
			expectedNamespace: "default",
		},
		"failed with empty request": {
			shouldErr: true,
		},
		"failed with empty service account token": {
			req:       &cpv1.CredentialProviderRequest{},
			shouldErr: true,
		},
		"failed with invalid service account token": {
			req: &cpv1.CredentialProviderRequest{
				ServiceAccountToken: "wrong",
			},
			shouldErr: true,
		},
		"failed with no kubernetes claim available": {
			req: &cpv1.CredentialProviderRequest{
				ServiceAccountToken: prepareToken(jwt.MapClaims{}),
			},
			shouldErr: true,
		},
		"failed with no kubernetes claim map": {
			req: &cpv1.CredentialProviderRequest{
				ServiceAccountToken: prepareToken(jwt.MapClaims{
					k8sClaimKey: "wrong",
				}),
			},
			shouldErr: true,
		},
		"failed with no namespace found": {
			req: &cpv1.CredentialProviderRequest{
				ServiceAccountToken: prepareToken(jwt.MapClaims{
					k8sClaimKey: map[string]any{},
				}),
			},
			shouldErr: true,
		},
		"failed with invalid namespace object": {
			req: &cpv1.CredentialProviderRequest{
				ServiceAccountToken: prepareToken(jwt.MapClaims{
					k8sClaimKey: map[string]any{
						"namespace": 1,
					},
				}),
			},
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			namespace, err := ExtractNamespace(tc.req)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedNamespace, namespace)
			}
		})
	}
}

func TestRetrieveSecrets(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		clientFunc    ClientFunc
		namespace     string
		setupClient   func() kubernetes.Interface
		shouldErr     bool
		expectedCount int
	}{
		"success with secrets": {
			namespace: "default",
			setupClient: func() kubernetes.Interface {
				return fake.NewClientset(
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret1",
							Namespace: "default",
						},
						Type: corev1.SecretTypeDockerConfigJson,
					},
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret2",
							Namespace: "default",
						},
						Type: corev1.SecretTypeDockerConfigJson,
					},
				)
			},
			expectedCount: 2,
		},
		"success with no secrets": {
			namespace: "empty",
			setupClient: func() kubernetes.Interface {
				return fake.NewClientset()
			},
			expectedCount: 0,
		},
		"error from client func": {
			namespace: "default",
			clientFunc: func(_ string) (kubernetes.Interface, error) {
				return nil, errors.New("client creation failed")
			},
			shouldErr: true,
		},
		"error listing secrets": {
			namespace: "default",
			setupClient: func() kubernetes.Interface {
				client := fake.NewClientset()
				// Inject an error by using a reactor that returns an error
				client.PrependReactor("list", "secrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("failed to list secrets")
				})

				return client
			},
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clientFunc := tc.clientFunc
			if clientFunc == nil && tc.setupClient != nil {
				client := tc.setupClient()
				clientFunc = func(_ string) (kubernetes.Interface, error) {
					return client, nil
				}
			}

			secrets, err := RetrieveSecrets(context.Background(), clientFunc, "test-token", tc.namespace)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, secrets.Items, tc.expectedCount)
			}
		})
	}
}

//nolint:paralleltest // Test manipulates global env vars and cannot run in parallel
func TestAPIServerHost(t *testing.T) {
	//nolint:paralleltest // Subtests manipulate global env vars and cannot run in parallel
	for name, tc := range map[string]struct {
		rootDir      string
		setupEnvFile bool
		envContent   string
		expected     string
	}{
		"success with absolute path and valid env file": {
			rootDir:      t.TempDir(),
			setupEnvFile: true,
			envContent:   "KUBERNETES_SERVICE_HOST=api.example.com\nKUBERNETES_SERVICE_PORT=6443",
			expected:     "api.example.com:6443",
		},
		"success with different port": {
			rootDir:      t.TempDir(),
			setupEnvFile: true,
			envContent:   "KUBERNETES_SERVICE_HOST=k8s.local\nKUBERNETES_SERVICE_PORT=8443",
			expected:     "k8s.local:8443",
		},
		"non-absolute path returns default": {
			rootDir:  "relative/path",
			expected: "localhost:6443",
		},
		"missing env file returns default": {
			rootDir:      t.TempDir(),
			setupEnvFile: false,
			expected:     "localhost:6443",
		},
		"empty env file returns colon only": {
			rootDir:      t.TempDir(),
			setupEnvFile: true,
			envContent:   "",
			expected:     ":",
		},
		"partial env vars host only": {
			rootDir:      t.TempDir(),
			setupEnvFile: true,
			envContent:   "KUBERNETES_SERVICE_HOST=partial.host",
			expected:     "partial.host:",
		},
		"partial env vars port only": {
			rootDir:      t.TempDir(),
			setupEnvFile: true,
			envContent:   "KUBERNETES_SERVICE_PORT=9443",
			expected:     ":9443",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Note: Not running subtests in parallel due to global env var manipulation

			// Save and clear env vars for clean test
			originalHost := os.Getenv("KUBERNETES_SERVICE_HOST")
			originalPort := os.Getenv("KUBERNETES_SERVICE_PORT")

			t.Cleanup(func() {
				if originalHost != "" {
					require.NoError(t, os.Setenv("KUBERNETES_SERVICE_HOST", originalHost)) //nolint:usetesting // Conditional restore
				} else {
					require.NoError(t, os.Unsetenv("KUBERNETES_SERVICE_HOST"))
				}

				if originalPort != "" {
					require.NoError(t, os.Setenv("KUBERNETES_SERVICE_PORT", originalPort)) //nolint:usetesting // Conditional restore
				} else {
					require.NoError(t, os.Unsetenv("KUBERNETES_SERVICE_PORT"))
				}
			})

			require.NoError(t, os.Unsetenv("KUBERNETES_SERVICE_HOST"))
			require.NoError(t, os.Unsetenv("KUBERNETES_SERVICE_PORT"))

			if tc.setupEnvFile {
				envFilePath := filepath.Join(tc.rootDir, "apiserver-url.env")
				err := os.WriteFile(envFilePath, []byte(tc.envContent), 0o600)
				require.NoError(t, err)
			}

			result := APIServerHost(tc.rootDir)
			assert.Equal(t, tc.expected, result)
		})
	}
}
