package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/cri-o/crio-credential-provider/internal/pkg/docker"
	cpAuth "github.com/cri-o/crio-credential-provider/pkg/auth"
)

func TestUpdateAuthContents(t *testing.T) {
	t.Parallel()

	secretUser := "su"
	secretPass := "sp"
	secretEncoded := base64.StdEncoding.EncodeToString([]byte(secretUser + ":" + secretPass))

	globalUser := "gu"
	globalPass := "gp"
	globalEncoded := base64.StdEncoding.EncodeToString([]byte(globalUser + ":" + globalPass))

	assertHas := func(contents docker.ConfigJSON, keys []string, expected string) {
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

	assertMissing := func(contents docker.ConfigJSON, keys []string) {
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
			name:           "mirror path with sub-namespaces match (with scheme in secret)",
			globalRegs:     []string{},
			secretRegs:     []string{"https://quay.io"},
			image:          "example.com/foo:tag",
			mirrors:        []string{"quay.io/foo"},
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
			name:           "no mirror or image matches in secret, returns global secret",
			globalRegs:     []string{"keep.io", "nomatch.local"},
			secretRegs:     []string{"quay.io"},
			image:          "nomatch.local/foo:tag",
			mirrors:        []string{"cache.local:5000"},
			wantGlobalRegs: []string{"keep.io", "nomatch.local"},
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			secrets := buildSecretList(t, secretEncoded, tt.secretRegs)
			globalContents := buildGlobalConfig(globalEncoded, tt.globalRegs)

			contents := updateAuthContents(secrets, globalContents, tt.image, tt.mirrors)

			assertHas(contents, tt.wantSecretRegs, secretEncoded)
			assertHas(contents, tt.wantGlobalRegs, globalEncoded)
			assertMissing(contents, tt.notWantRegs)
		})
	}
}

func TestCreateAuthFile(t *testing.T) {
	t.Parallel()

	user := "u1"
	pass := "p1"
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))

	cfg := docker.ConfigJSON{Auths: map[string]docker.AuthConfig{
		"quay.io":          {Auth: auth},
		"cache.local:5000": {Auth: auth},
		"registry.local":   {Auth: auth},
	}}

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

	secrets := &corev1.SecretList{Items: []corev1.Secret{secret}}

	namespace := "ns-unit"
	image := "registry.local/app/img:1"
	mirrors := []string{"mirror.quay.io", "cache.local:5000", "quay.io"}

	authDir := t.TempDir()

	path, err := CreateAuthFile(secrets, "", authDir, namespace, image, mirrors)
	if err != nil {
		t.Fatalf("CreateAuthFile error: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(path) })

	wantPath, err := cpAuth.FilePath(authDir, namespace, image)
	require.NoError(t, err)
	assert.Equal(t, wantPath, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var written docker.ConfigJSON

	err = json.Unmarshal(data, &written)
	require.NoError(t, err)

	// Expect entries for quay.io (mirror match) and registry.local (image match)
	assert.Len(t, written.Auths, len(cfg.Auths))
	assert.Contains(t, written.Auths, "quay.io")
	assert.Contains(t, written.Auths, "registry.local")
	assert.Contains(t, written.Auths, "cache.local:5000")
}

func buildSecretList(t *testing.T, encoded string, regs []string) *corev1.SecretList {
	t.Helper()

	cfg := docker.ConfigJSON{Auths: map[string]docker.AuthConfig{}}
	for _, r := range regs {
		cfg.Auths[r] = docker.AuthConfig{Auth: encoded}
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

func buildGlobalConfig(encoded string, regs []string) docker.ConfigJSON {
	if len(regs) == 0 {
		return docker.ConfigJSON{}
	}

	g := docker.ConfigJSON{Auths: map[string]docker.AuthConfig{}}
	for _, reg := range regs {
		g.Auths[reg] = docker.AuthConfig{Auth: encoded}
	}

	return g
}

func TestReadGlobalAuthFile(t *testing.T) {
	t.Parallel()

	// Create a temporary auth.json
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")

	conf := `{
	"auths": {
		"docker.io": {
			"auth": "Z3U6Z3A="
		},
		"registry.redhat.io": {
			"auth": "Z3U6Z3A="
		}
	}
}
`
	if err := os.WriteFile(authPath, []byte(conf), 0o600); err != nil {
		t.Fatalf("failed to write temp auth.json: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(authPath) })

	contents, err := readGlobalAuthFile(authPath)
	require.NoError(t, err)
	// Expect 2 entries
	assert.Len(t, contents.Auths, 2)
	assert.Contains(t, contents.Auths, "docker.io")
	assert.Contains(t, contents.Auths, "registry.redhat.io")
	assert.Equal(t, "Z3U6Z3A=", contents.Auths["docker.io"].Auth)
	assert.Equal(t, "Z3U6Z3A=", contents.Auths["registry.redhat.io"].Auth)

	nonexistPath := "/nonexistent/auth.json"
	contents, err = readGlobalAuthFile(nonexistPath)
	require.NoError(t, err)
	assert.Empty(t, contents.Auths)

	invalidPath := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("failed to write invalid json: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(invalidPath) })

	_, err = readGlobalAuthFile(invalidPath)
	require.Error(t, err)
}

func TestValidDockerConfigSecret(t *testing.T) {
	t.Parallel()

	validAuth := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	cfg := docker.ConfigJSON{Auths: map[string]docker.AuthConfig{
		"quay.io": {Auth: validAuth},
	}}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		secret    corev1.Secret
		shouldErr bool
	}{
		"valid docker config secret": {
			secret: corev1.Secret{
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: cfgBytes,
				},
			},
			shouldErr: false,
		},
		"wrong secret type": {
			secret: corev1.Secret{
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: cfgBytes,
				},
			},
			shouldErr: true,
		},
		"missing docker config key": {
			secret: corev1.Secret{
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					"wrong-key": cfgBytes,
				},
			},
			shouldErr: true,
		},
		"invalid json in docker config": {
			secret: corev1.Secret{
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte("invalid json"),
				},
			},
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := validDockerConfigSecret(tc.secret)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, result.Auths)
			}
		})
	}
}

func TestDecodeDockerAuth(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		auth         docker.AuthConfig
		shouldErr    bool
		expectUser   string
		expectPasswd string
	}{
		"valid base64 auth": {
			auth:         docker.AuthConfig{Auth: base64.StdEncoding.EncodeToString([]byte("user:password"))},
			shouldErr:    false,
			expectUser:   "user",
			expectPasswd: "password",
		},
		"valid auth with null terminator": {
			auth:         docker.AuthConfig{Auth: base64.StdEncoding.EncodeToString([]byte("user:pass\x00"))},
			shouldErr:    false,
			expectUser:   "user",
			expectPasswd: "pass",
		},
		"invalid base64": {
			auth:      docker.AuthConfig{Auth: "not-valid-base64!!!"},
			shouldErr: true,
		},
		"missing colon separator": {
			auth:         docker.AuthConfig{Auth: base64.StdEncoding.EncodeToString([]byte("userpassword"))},
			shouldErr:    false,
			expectUser:   "",
			expectPasswd: "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			entry, err := decodeDockerAuth(tc.auth)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectUser, entry.Username)
				assert.Equal(t, tc.expectPasswd, entry.Password)
			}
		})
	}
}

func TestWriteAuthFile(t *testing.T) {
	t.Parallel()

	auth := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	validContents := docker.ConfigJSON{
		Auths: map[string]docker.AuthConfig{
			"quay.io": {Auth: auth},
		},
	}

	for name, tc := range map[string]struct {
		contents  docker.ConfigJSON
		shouldErr bool
	}{
		"valid contents": {
			contents:  validContents,
			shouldErr: false,
		},
		"empty auths": {
			contents:  docker.ConfigJSON{Auths: map[string]docker.AuthConfig{}},
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			path, err := writeAuthFile(dir, "test-image", "test-ns", tc.contents)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, path)
				assert.FileExists(t, path)

				data, err := os.ReadFile(path)
				require.NoError(t, err)

				var written docker.ConfigJSON

				err = json.Unmarshal(data, &written)
				require.NoError(t, err)
				assert.Equal(t, tc.contents.Auths, written.Auths)
			}
		})
	}
}

func TestCreateAuthFileErrors(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		secrets         *corev1.SecretList
		namespace       string
		globalAuthPath  string
		authDir         string
		setupAuthDir    func(t *testing.T) string
		setupGlobalAuth func(t *testing.T) string
		shouldErr       bool
		errorContains   string
	}{
		"empty namespace": {
			secrets:   &corev1.SecretList{},
			namespace: "",
			shouldErr: true,
		},
		"nil secrets": {
			secrets:   nil,
			namespace: "test",
			shouldErr: true,
		},
		"error reading global auth file": {
			secrets:   &corev1.SecretList{},
			namespace: "test",
			setupGlobalAuth: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				authPath := filepath.Join(dir, "auth.json")
				// Create directory instead of file to cause read error
				err := os.Mkdir(authPath, 0o755)
				require.NoError(t, err)

				return authPath
			},
			shouldErr: true,
		},
		"error with no matching auths": {
			secrets: &corev1.SecretList{
				Items: []corev1.Secret{
					{
						Type: corev1.SecretTypeDockerConfigJson,
						Data: map[string][]byte{
							corev1.DockerConfigJsonKey: []byte(`{"auths":{"other.io":{"auth":"dGVzdDp0ZXN0"}}}`),
						},
					},
				},
			},
			namespace: "test",
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tc.setupAuthDir != nil {
				dir = tc.setupAuthDir(t)
			}

			globalAuthPath := ""
			if tc.setupGlobalAuth != nil {
				globalAuthPath = tc.setupGlobalAuth(t)
			}

			_, err := CreateAuthFile(tc.secrets, globalAuthPath, dir, tc.namespace, "test-image", []string{"mirror.io"})
			if tc.shouldErr {
				require.Error(t, err)

				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUpdateAuthContentsNoMatches(t *testing.T) {
	t.Parallel()

	secrets := &corev1.SecretList{
		Items: []corev1.Secret{
			{
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{},
			},
		},
	}

	globalContents := docker.ConfigJSON{
		Auths: map[string]docker.AuthConfig{
			"global.io": {Auth: "Z3VlOnBhc3M="},
		},
	}

	result := updateAuthContents(secrets, globalContents, "test.io/image", []string{"mirror.io"})

	// Should preserve global auths when no matching secrets
	assert.Len(t, result.Auths, 1)
	assert.Contains(t, result.Auths, "global.io")
}

func TestNormalizeSecretRegistry(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		input    string
		expected string
	}{
		"http prefix": {
			input:    "http://registry.io",
			expected: "registry.io",
		},
		"https prefix": {
			input:    "https://registry.io",
			expected: "registry.io",
		},
		"no prefix": {
			input:    "registry.io",
			expected: "registry.io",
		},
		"with port": {
			input:    "https://registry.io:5000",
			expected: "registry.io:5000",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := normalizeSecretRegistry(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
