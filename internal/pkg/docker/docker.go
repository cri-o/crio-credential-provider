// Package docker contains types specific to authenticate against container registries.
package docker

// ConfigJSON represents ~/.docker/config.json file info.
type ConfigJSON struct {
	// Auths maps a registry prefix to an AuthConfig instance.
	Auths map[string]AuthConfig `json:"auths"`
}

// AuthConfig is a single registry's auth configuration.
type AuthConfig struct {
	// AUth is the base64 encoded credential in the format user:password.
	Auth string `json:"auth,omitempty"`
}

// ConfigEntry wraps a docker config as a entry.
type ConfigEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
