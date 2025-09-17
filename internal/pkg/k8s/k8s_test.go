package k8s

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

func TestExtractNamespace(t *testing.T) {
	t.Parallel()

	prepareToken := func(claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)

		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

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
