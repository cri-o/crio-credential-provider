package mirrors

import (
	"os"
	"path/filepath"
	"testing"

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

	req := &cpv1.CredentialProviderRequest{Image: "quay.io/library/nginx"}

	mirrors, err := Match(req, confPath)
	if err != nil {
		t.Fatalf("matchMirrors returned error: %v", err)
	}

	if len(mirrors) != 2 {
		t.Fatalf("expected 2 mirrors, got %d: %#v", len(mirrors), mirrors)
	}

	if mirrors[0] != "mirror.quay.io" || mirrors[1] != "cache.local:5000" {
		t.Fatalf("unexpected mirrors order/content: %#v", mirrors)
	}
}
