package mirrors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cpv1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

func TestMatchMirrors(t *testing.T) {
	t.Parallel()

	// Create a temporary registries.conf
	dir := t.TempDir()
	confPath := filepath.Join(dir, "registries.conf")

	conf := `unqualified-search-registries = ["quay.io"]

[[registry]]
location = "quay.io"

  [[registry.mirror]]
  location = "mirror.quay.io"

  [[registry.mirror]]
  location = "cache.local:5000"
`
	err := os.WriteFile(confPath, []byte(conf), 0o600)
	require.NoError(t, err)

	req := &cpv1.CredentialProviderRequest{Image: "quay.io/library/nginx"}

	mirrors, err := Match(req, confPath)
	require.NoError(t, err)

	assert.Len(t, mirrors, 2)
	assert.Contains(t, mirrors, "mirror.quay.io")
	assert.Contains(t, mirrors, "cache.local:5000")
}

func TestMatchEdgeCases(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		req       *cpv1.CredentialProviderRequest
		confPath  string
		setupConf func(t *testing.T) string
		shouldErr bool
		expectLen int
		expectNil bool
	}{
		"nil request": {
			req:       nil,
			confPath:  "/tmp/registries.conf",
			shouldErr: true,
		},
		"empty image": {
			req:       &cpv1.CredentialProviderRequest{Image: ""},
			confPath:  "/tmp/registries.conf",
			shouldErr: true,
		},
		"no registry found": {
			req: &cpv1.CredentialProviderRequest{Image: "docker.io/library/nginx"},
			setupConf: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				confPath := filepath.Join(dir, "registries.conf")
				conf := `unqualified-search-registries = ["docker.io"]`
				err := os.WriteFile(confPath, []byte(conf), 0o600)
				require.NoError(t, err)

				return confPath
			},
			expectNil: true,
		},
		"registry with no mirrors": {
			req: &cpv1.CredentialProviderRequest{Image: "gcr.io/library/nginx"},
			setupConf: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				confPath := filepath.Join(dir, "registries.conf")
				conf := `[[registry]]
location = "gcr.io"`
				err := os.WriteFile(confPath, []byte(conf), 0o600)
				require.NoError(t, err)

				return confPath
			},
			expectLen: 0,
		},
		"invalid registries conf path": {
			req:       &cpv1.CredentialProviderRequest{Image: "quay.io/test/image"},
			confPath:  "/nonexistent/path/registries.conf",
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			confPath := tc.confPath
			if tc.setupConf != nil {
				confPath = tc.setupConf(t)
			}

			mirrors, err := Match(tc.req, confPath)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				if tc.expectNil {
					assert.Nil(t, mirrors)
				} else {
					assert.Len(t, mirrors, tc.expectLen)
				}
			}
		})
	}
}
