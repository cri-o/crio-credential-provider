package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		message string
	}{
		"success": {
			message: "Hello world",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			l, err := New()
			require.NoError(t, err)
			l.Print(tc.message)

			executable, err := os.Executable()
			require.NoError(t, err)

			logFileContents, err := os.ReadFile(filepath.Join(executable, "../../logs"))
			require.NoError(t, err)

			assert.Contains(t, string(logFileContents), tc.message)
		})
	}
}
