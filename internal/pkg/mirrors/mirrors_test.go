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
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatalf("failed to write temp registries.conf: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(confPath) })

	req := &cpv1.CredentialProviderRequest{Image: "quay.io/library/nginx"}

	mirrors, err := Match(req, confPath)
	require.NoError(t, err)

	assert.Len(t, mirrors, 2)
	assert.Contains(t, mirrors, "mirror.quay.io")
	assert.Contains(t, mirrors, "cache.local:5000")
}
