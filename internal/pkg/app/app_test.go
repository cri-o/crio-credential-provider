package app

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"

	"github.com/cri-o/credential-provider/internal/pkg/docker"
	"github.com/cri-o/credential-provider/internal/pkg/k8s"
)

const (
	k8sClaimKey            = "kubernetes.io"
	namespace              = "default"
	registry               = "docker.io"
	image                  = registry + "/library/image"
	mirror                 = "localhost:5000"
	usernamePasswordBase64 = "bXl1c2VyOm15cGFzc3dvcmQ="
)

var imageHash = sha256.Sum256([]byte(image))

func prepareToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tokenString, err := token.SignedString(key)
	require.NoError(t, err)

	return tokenString
}

func TestRun(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		prepare func() (buffer *bytes.Buffer, registriesConfPath, authDir, kubeletAuthFilePath string, client k8s.ClientFunc)
		assert  func(err error, authDir string)
	}{
		"success": {
			prepare: func() (*bytes.Buffer, string, string, string, k8s.ClientFunc) {
				tempDir := t.TempDir()
				registriesConf, err := os.CreateTemp(tempDir, "")
				require.NoError(t, err)

				_, err = fmt.Fprintf(registriesConf, "[[registry]]\nlocation = %q\n[[registry.mirror]]\nlocation = %q", registry, mirror)
				require.NoError(t, err)

				buffer := &bytes.Buffer{}
				serviceAccountToken := prepareToken(t, jwt.MapClaims{
					k8sClaimKey: map[string]any{"namespace": namespace},
				})
				req := &cpv1.CredentialProviderRequest{
					Image:               image,
					ServiceAccountToken: serviceAccountToken,
				}
				res, err := json.Marshal(req)
				require.NoError(t, err)

				_, err = buffer.Write(res)
				require.NoError(t, err)

				clientFunc := func(string) (kubernetes.Interface, error) {
					return fake.NewSimpleClientset(&corev1.SecretList{Items: []corev1.Secret{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "secret",
								Namespace: namespace,
							},
							Type: corev1.SecretTypeDockerConfigJson,
							Data: map[string][]byte{
								corev1.DockerConfigJsonKey: fmt.Appendf([]byte{},
									`{"auths":{"http://%s":{"username":"myuser","password":"mypassword","auth":%q}}}`,
									mirror, usernamePasswordBase64,
								),
							},
						},
					}}), nil
				}

				return buffer,
					registriesConf.Name(),
					tempDir,
					filepath.Join(tempDir, "kubelet-auth.json"),
					clientFunc
			},
			assert: func(err error, authDir string) {
				require.NoError(t, err)

				path := filepath.Join(authDir, fmt.Sprintf("%s-%x.json", namespace, imageHash))
				require.FileExists(t, path)

				authFileContents, err := os.ReadFile(path)
				require.NoError(t, err)

				authConfig := docker.ConfigJSON{}
				err = json.Unmarshal(authFileContents, &authConfig)
				require.NoError(t, err)

				require.Len(t, authConfig.Auths, 1)
				require.Equal(t, usernamePasswordBase64, authConfig.Auths[mirror].Auth)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			buffer, registriesConfPath, authDir, kubeletAuthFilePath, clientFunc := tc.prepare()

			err := Run(buffer, registriesConfPath, authDir, kubeletAuthFilePath, clientFunc)

			tc.assert(err, authDir)
		})
	}
}
