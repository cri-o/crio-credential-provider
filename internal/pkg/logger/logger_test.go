package logger

import (
	"bytes"
	"io"
	"os"
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

			prevStdErr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			L().Print(tc.message)
			L().Print(tc.message)

			require.NoError(t, w.Close())

			buf := bytes.Buffer{}

			_, err := io.Copy(&buf, r)
			require.NoError(t, err)

			os.Stderr = prevStdErr

			assert.Contains(t, buf.String(), tc.message)
		})
	}
}
