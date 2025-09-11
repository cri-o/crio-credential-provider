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

func TestMatchMirrors_WithRegistriesConf(t *testing.T) {
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

func TestFindDockerAuthFromSecrets(t *testing.T) {
	// Provided dockerConfigJSONBytes (base64 of the dockerconfigjson content) mirror.quay.io, myname:mypassword
	testdockerConfigJSONBytes := "ewoJImF1dGhzIjogewoJCSJtaXJyb3IucXVheS5pbyI6IHsKCQkJImF1dGgiOiAiYlhsdVlXMWxPbTE1Y0dGemMzZHZjbVE9IgoJCX0KCX0KfQ=="

	decoded, err := base64.StdEncoding.DecodeString(testdockerConfigJSONBytes)
	if err != nil {
		t.Fatalf("failed to decode test payload: %v", err)
	}

	secret := corev1.Secret{
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: decoded,
		},
	}
	secrets := &corev1.SecretList{Items: []corev1.Secret{secret}}

	// Mirrors include mirror.quay.io to match the first entry key
	mirrors := []string{"mirror.quay.io"}

	logger := log.New(os.Stderr, "", log.LstdFlags)

	entry := findDockerAuthFromSecrets(logger, secrets, "", mirrors)
	if entry == nil {
		t.Fatalf("expected an auth entry, got nil")
	}

	// The provided auth values decode to username:password; verify both
	if entry.Username != "myname" {
		t.Errorf("decoded username is %q, expected %q", entry.Username, "myname")
	}

	if entry.Password != "mypassword" {
		t.Errorf("decoded password is %q, expected %q", entry.Password, "mypassword")
	}
}

func TestCreateAuthFile(t *testing.T) {
	user := "u1"
	pass := "p1"
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))

	cfg := DockerConfigJSON{Auths: map[string]DockerAuthConfig{
		"quay.io":          {Auth: auth},
		"cache.local:5000": {Auth: auth},
		"registry.local":   {Auth: auth},
	}}
	cfgBytes, _ := json.Marshal(cfg)

	secret := corev1.Secret{
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: cfgBytes,
		},
	}
	log.Println("secret", string(cfgBytes))
	secrets := &corev1.SecretList{Items: []corev1.Secret{secret}}

	logger := log.New(os.Stderr, "", 0)
	namespace := "ns-unit"
	image := "registry.local/app/img:1"
	mirrors := []string{"mirror.quay.io", "cache.local:5000", "quay.io"}

	path, err := CreateAuthFile(logger, secrets, namespace, image, mirrors)
	if err != nil {
		t.Fatalf("CreateAuthFile error: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })

	if wantPath := filepath.Join("/tmp", namespace+"-auth.json"); path != wantPath {
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
