package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()

	info, err := Get()
	require.NoError(t, err)
	assert.NotNil(t, info)

	assert.Equal(t, Version, info.Version)

	versionString := info.String()
	assert.NotEmpty(t, versionString)

	jsonString, err := info.JSONString()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonString)
}
