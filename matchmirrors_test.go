package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
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

	mirrors, err := matchMirrors(req, confPath)
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

func TestUpdateAuthContents(t *testing.T) {
	secretUser := "su"
	secretPass := "sp"
	secretEncoded := base64.StdEncoding.EncodeToString([]byte(secretUser + ":" + secretPass))

	globalUser := "gu"
	globalPass := "gp"
	globalEncoded := base64.StdEncoding.EncodeToString([]byte(globalUser + ":" + globalPass))

	assertHas := func(contents DockerConfigJSON, keys []string, expected string) {
		for _, k := range keys {
			v, ok := contents.Auths[k]
			if !ok {
				t.Fatalf("expected key %q in contents auths: %#v", k, contents.Auths)
			}

			if v.Auth != expected {
				t.Fatalf("expected key %q to have auth %q, got %q", k, expected, v.Auth)
			}
		}
	}

	assertMissing := func(contents DockerConfigJSON, keys []string) {
		for _, k := range keys {
			if _, ok := contents.Auths[k]; ok {
				t.Fatalf("did not expect key %q in contents auths: %#v", k, contents.Auths)
			}
		}
	}

	tests := []struct {
		name           string
		globalRegs     []string
		secretRegs     []string
		image          string
		mirrors        []string
		wantSecretRegs []string // should exist with secretEncoded
		wantGlobalRegs []string // should exist with globalEncoded (not overwritten)
		notWantRegs    []string // should not exist
		wantErr        bool
	}{
		{
			name:           "merge secrets over global and preserve others",
			globalRegs:     []string{"global.io", "registry.local"},
			secretRegs:     []string{"quay.io", "cache.local:5000", "registry.local"},
			image:          "registry.local/app/img:1",
			mirrors:        []string{"mirror.quay.io", "cache.local:5000", "quay.io"},
			wantSecretRegs: []string{"quay.io", "cache.local:5000", "registry.local"},
			wantGlobalRegs: []string{"global.io"},
		},
		{
			name:           "mirror-only match (with scheme in secret)",
			globalRegs:     []string{},
			secretRegs:     []string{"https://quay.io"},
			image:          "example.com/foo:tag",
			mirrors:        []string{"quay.io"},
			wantSecretRegs: []string{"quay.io"},
		},
		{
			name:           "image-only match (with scheme in secret)",
			globalRegs:     []string{},
			secretRegs:     []string{"http://registry.local"},
			image:          "registry.local/foo:tag",
			mirrors:        []string{"quay.io"},
			wantSecretRegs: []string{"registry.local"},
			notWantRegs:    []string{"quay.io"},
		},
		{
			name:       "no matches returns error",
			globalRegs: []string{"keep.io"},
			secretRegs: []string{"quay.io"},
			image:      "nomatch.local/foo:tag",
			mirrors:    []string{"cache.local:5000"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.New(os.Stderr, "", 0)
			secrets := buildSecretList(t, secretEncoded, tt.secretRegs)
			globalContents := buildGlobalConfig(globalEncoded, tt.globalRegs)

			contents, err := updateAuthContents(logger, secrets, globalContents, tt.image, tt.mirrors)
			if err == nil && tt.wantErr {
				t.Fatalf("expected error, got nil")
			}

			if err != nil && !tt.wantErr {
				t.Fatalf("updateAuthContents error: %v", err)
			}

			if tt.wantErr {
				return
			}

			assertHas(contents, tt.wantSecretRegs, secretEncoded)
			assertHas(contents, tt.wantGlobalRegs, globalEncoded)
			assertMissing(contents, tt.notWantRegs)
		})
	}
}

func TestCreateAuthFile(t *testing.T) {
	authDir = t.TempDir()

	user := "u1"
	pass := "p1"
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))

	cfg := DockerConfigJSON{Auths: map[string]DockerAuthConfig{
		"quay.io":          {Auth: auth},
		"cache.local:5000": {Auth: auth},
		"registry.local":   {Auth: auth},
	}}

	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marhsal auth config: %v", err)
	}

	secret := corev1.Secret{
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: cfgBytes,
		},
	}

	secrets := &corev1.SecretList{Items: []corev1.Secret{secret}}

	logger := log.New(os.Stderr, "", 0)
	namespace := "ns-unit"
	image := "registry.local/app/img:1"
	mirrors := []string{"mirror.quay.io", "cache.local:5000", "quay.io"}

	path, err := createAuthFile(logger, secrets, "", authDir, namespace, image, mirrors)
	if err != nil {
		t.Fatalf("CreateAuthFile error: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(path) })

	if wantPath := filepath.Join(authDir, namespace+".json"); path != wantPath {
		t.Fatalf("unexpected path: got %q want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var written DockerConfigJSON
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Expect entries for quay.io (mirror match) and registry.local (image match)
	if _, ok := written.Auths["quay.io"]; !ok {
		t.Fatalf("expected quay.io entry in written auths: %#v", written.Auths)
	}

	if _, ok := written.Auths["registry.local"]; !ok {
		t.Fatalf("expected registry.local entry in written auths: %#v", written.Auths)
	}

	if _, ok := written.Auths["cache.local:5000"]; !ok {
		t.Fatalf("expected cache.local:5000 entry in written auths: %#v", written.Auths)
	}
}

func buildSecretList(t *testing.T, encoded string, regs []string) *corev1.SecretList {
	t.Helper()

	cfg := DockerConfigJSON{Auths: map[string]DockerAuthConfig{}}
	for _, r := range regs {
		cfg.Auths[r] = DockerAuthConfig{Auth: encoded}
	}

	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal auth config: %v", err)
	}

	secret := corev1.Secret{
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: cfgBytes,
		},
	}

	return &corev1.SecretList{Items: []corev1.Secret{secret}}
}

func buildGlobalConfig(encoded string, regs []string) DockerConfigJSON {
	if len(regs) == 0 {
		return DockerConfigJSON{}
	}

	g := DockerConfigJSON{Auths: map[string]DockerAuthConfig{}}
	for _, reg := range regs {
		g.Auths[reg] = DockerAuthConfig{Auth: encoded}
	}

	return g
}
