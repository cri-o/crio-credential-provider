package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	t.Parallel()

	info, err := Get()
	require.NoError(t, err)
	assert.NotNil(t, info)

	assert.Equal(t, Version, info.Version)
	assert.NotEmpty(t, info.GoVersion)
	assert.NotEmpty(t, info.Compiler)
	assert.NotEmpty(t, info.Platform)
}

func TestString(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		info     Info
		expected []string
	}{
		"full info": {
			info: Info{
				Version:       "1.0.0",
				GitCommit:     "abc123",
				GitCommitDate: "2025-01-01",
				BuildDate:     "2025-01-02",
				GoVersion:     "go1.21",
				Compiler:      "gc",
				Platform:      "linux/amd64",
				LDFlags:       "-X main.version=1.0.0",
				Dependencies:  []string{"dep1 v1.0.0", "dep2 v2.0.0"},
			},
			expected: []string{"Version:", "1.0.0", "GitCommit:", "abc123", "Dependencies:", "dep1 v1.0.0", "dep2 v2.0.0"},
		},
		"minimal info": {
			info: Info{
				Version: "1.0.0",
			},
			expected: []string{"Version:", "1.0.0"},
		},
		"empty strings filtered out": {
			info: Info{
				Version:   "1.0.0",
				GitCommit: "",
				BuildDate: "   ",
			},
			expected: []string{"Version:", "1.0.0"},
		},
		"with dependencies": {
			info: Info{
				Version:      "1.0.0",
				Dependencies: []string{"github.com/foo/bar v1.2.3 h1:abc"},
			},
			expected: []string{"Version:", "Dependencies:", "github.com/foo/bar"},
		},
		"empty dependencies": {
			info: Info{
				Version:      "1.0.0",
				Dependencies: []string{},
			},
			expected: []string{"Version:", "1.0.0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := tc.info.String()
			for _, exp := range tc.expected {
				assert.Contains(t, result, exp)
			}
		})
	}
}

func TestJSONString(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		info      Info
		shouldErr bool
	}{
		"valid info": {
			info: Info{
				Version:       "1.0.0",
				GitCommit:     "abc123",
				GitCommitDate: "2025-01-01",
				BuildDate:     "2025-01-02",
				GoVersion:     "go1.21",
				Compiler:      "gc",
				Platform:      "linux/amd64",
				LDFlags:       "-X main.version=1.0.0",
				Dependencies:  []string{"dep1 v1.0.0", "dep2 v2.0.0"},
			},
			shouldErr: false,
		},
		"minimal info": {
			info: Info{
				Version: "1.0.0",
			},
			shouldErr: false,
		},
		"empty info": {
			info:      Info{},
			shouldErr: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			jsonString, err := tc.info.JSONString()
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, jsonString)
				assert.Contains(t, jsonString, "{")
				assert.Contains(t, jsonString, "}")
			}
		})
	}
}
