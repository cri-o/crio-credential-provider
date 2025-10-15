package docker

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigJSON(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		jsonInput    string
		expectAuths  int
		expectFields map[string]string
	}{
		"valid config with single auth": {
			jsonInput: `{
				"auths": {
					"quay.io": {
						"auth": "dXNlcjpwYXNz"
					}
				}
			}`,
			expectAuths: 1,
			expectFields: map[string]string{
				"quay.io": "dXNlcjpwYXNz",
			},
		},
		"valid config with multiple auths": {
			jsonInput: `{
				"auths": {
					"quay.io": {
						"auth": "dXNlcjE6cGFzczE="
					},
					"docker.io": {
						"auth": "dXNlcjI6cGFzczI="
					}
				}
			}`,
			expectAuths: 2,
			expectFields: map[string]string{
				"quay.io":   "dXNlcjE6cGFzczE=",
				"docker.io": "dXNlcjI6cGFzczI=",
			},
		},
		"empty config": {
			jsonInput:   `{}`,
			expectAuths: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var cfg ConfigJSON

			err := json.Unmarshal([]byte(tc.jsonInput), &cfg)
			require.NoError(t, err)

			if tc.expectAuths > 0 {
				assert.Len(t, cfg.Auths, tc.expectAuths)

				for registry, expectedAuth := range tc.expectFields {
					auth, ok := cfg.Auths[registry]
					require.True(t, ok, "expected registry %s to be present", registry)
					assert.Equal(t, expectedAuth, auth.Auth)
				}
			} else {
				assert.Empty(t, cfg.Auths)
			}
		})
	}
}

func TestAuthConfig(t *testing.T) {
	t.Parallel()

	auth := AuthConfig{
		Auth: "dXNlcjpwYXNz",
	}

	data, err := json.Marshal(auth)
	require.NoError(t, err)

	var decoded AuthConfig

	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, auth.Auth, decoded.Auth)
}

func TestConfigEntry(t *testing.T) {
	t.Parallel()

	entry := ConfigEntry{
		Username: "testuser",
		Password: "testpass",
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var decoded ConfigEntry

	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entry.Username, decoded.Username)
	assert.Equal(t, entry.Password, decoded.Password)
}

func TestConfigJSONMarshaling(t *testing.T) {
	t.Parallel()

	cfg := ConfigJSON{
		Auths: map[string]AuthConfig{
			"quay.io": {
				Auth: "dXNlcjpwYXNz",
			},
			"docker.io": {
				Auth: "YWRtaW46c2VjcmV0",
			},
		},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ConfigJSON

	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Len(t, decoded.Auths, 2)
	assert.Equal(t, cfg.Auths["quay.io"].Auth, decoded.Auths["quay.io"].Auth)
	assert.Equal(t, cfg.Auths["docker.io"].Auth, decoded.Auths["docker.io"].Auth)
}
