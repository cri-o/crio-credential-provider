package app

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
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

	"github.com/cri-o/crio-credential-provider/internal/pkg/docker"
	"github.com/cri-o/crio-credential-provider/internal/pkg/k8s"
	"github.com/cri-o/crio-credential-provider/pkg/auth"
)

const (
	k8sClaimKey            = "kubernetes.io"
	namespace              = "default"
	registry               = "docker.io"
	image                  = registry + "/library/image"
	mirror                 = "localhost:5000"
	usernamePasswordBase64 = "bXl1c2VyOm15cGFzc3dvcmQ="
)

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

	requestBuffer := func(includeNamespace bool) *bytes.Buffer {
		buffer := &bytes.Buffer{}

		claims := jwt.MapClaims{}
		if includeNamespace {
			claims = jwt.MapClaims{k8sClaimKey: map[string]any{"namespace": namespace}}
		}

		serviceAccountToken := prepareToken(t, claims)
		req := &cpv1.CredentialProviderRequest{
			Image:               image,
			ServiceAccountToken: serviceAccountToken,
		}
		res, err := json.Marshal(req)
		require.NoError(t, err)

		_, err = buffer.Write(res)
		require.NoError(t, err)

		return buffer
	}

	tempDirWithRegistriesConf := func() (string, *os.File) {
		tempDir := t.TempDir()
		registriesConf, err := os.CreateTemp(tempDir, "")
		require.NoError(t, err)

		return tempDir, registriesConf
	}

	for name, tc := range map[string]struct {
		prepare func() (buffer *bytes.Buffer, registriesConfPath, authDir string, client k8s.ClientFunc)
		assert  func(err error, authDir string)
	}{
		"success": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir, registriesConf := tempDirWithRegistriesConf()

				_, err := fmt.Fprintf(registriesConf, "[[registry]]\nlocation = %q\n[[registry.mirror]]\nlocation = %q", registry, mirror)
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

				return requestBuffer(true),
					registriesConf.Name(),
					tempDir,
					clientFunc
			},
			assert: func(err error, authDir string) {
				require.NoError(t, err)

				path, err := auth.FilePath(authDir, namespace, image)
				require.NoError(t, err)
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
		"success no mirrors": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir, registriesConf := tempDirWithRegistriesConf()

				return requestBuffer(true),
					registriesConf.Name(),
					tempDir,
					nil
			},
			assert: func(err error, authDir string) {
				require.NoError(t, err)

				path, err := auth.FilePath(authDir, namespace, image)
				require.NoError(t, err)
				require.NoFileExists(t, path)
			},
		},
		"success missing registries.conf": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir, _ := tempDirWithRegistriesConf()

				return &bytes.Buffer{},
					"",
					tempDir,
					nil
			},
			assert: func(err error, _ string) {
				require.NoError(t, err)
			},
		},
		"failure on secrets retrieval": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir, registriesConf := tempDirWithRegistriesConf()

				_, err := fmt.Fprintf(registriesConf, "[[registry]]\nlocation = %q\n[[registry.mirror]]\nlocation = %q", registry, mirror)
				require.NoError(t, err)

				clientFunc := func(string) (kubernetes.Interface, error) {
					return nil, errors.New("error")
				}

				return requestBuffer(true),
					registriesConf.Name(),
					tempDir,
					clientFunc
			},
			assert: func(err error, _ string) {
				require.Error(t, err)
			},
		},
		"failure on match mirrors": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir, registriesConf := tempDirWithRegistriesConf()

				_, err := fmt.Fprint(registriesConf, "wrong content")
				require.NoError(t, err)

				return requestBuffer(true),
					registriesConf.Name(),
					tempDir,
					nil
			},
			assert: func(err error, _ string) {
				require.Error(t, err)
			},
		},
		"failure on extract namespace": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir := t.TempDir()
				registriesConf, err := os.CreateTemp(tempDir, "")
				require.NoError(t, err)

				return requestBuffer(false),
					registriesConf.Name(),
					tempDir,
					nil
			},
			assert: func(err error, _ string) {
				require.Error(t, err)
			},
		},
		"failure on unmarshal request": {
			prepare: func() (*bytes.Buffer, string, string, k8s.ClientFunc) {
				tempDir, registriesConf := tempDirWithRegistriesConf()

				return &bytes.Buffer{},
					registriesConf.Name(),
					tempDir,
					nil
			},
			assert: func(err error, _ string) {
				require.Error(t, err)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			buffer, registriesConfPath, authDir, clientFunc := tc.prepare()
			kubeletAuthFilePath := filepath.Join(authDir, "kubelet-auth.json")

			err := Run(buffer, registriesConfPath, authDir, kubeletAuthFilePath, clientFunc)

			tc.assert(err, authDir)
		})
	}
}
