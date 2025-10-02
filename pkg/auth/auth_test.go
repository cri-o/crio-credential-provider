package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilePath(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		dir, namespace, imageRef, expectedRes, expectedErr string
	}{
		"success": {
			dir:         "/some/dir",
			namespace:   "namespace",
			imageRef:    "image:latest",
			expectedRes: "/some/dir/namespace-baee713fe56d0f5189067a1126374bc39cd8bbca1cd980322f0c8596cd400826.json",
		},
		"failure no absolute path": {
			dir:         "dir",
			namespace:   "namespace",
			imageRef:    "image:latest",
			expectedErr: "provided \"dir\" directory is not an absolute path",
		},
		"failure no dir provided": {
			dir:         "",
			namespace:   "namespace",
			imageRef:    "image:latest",
			expectedErr: "provided \"\" directory is not an absolute path",
		},
		"failure no namespace provided": {
			dir:         "/some/dir",
			namespace:   "",
			imageRef:    "image:latest",
			expectedErr: "no namespace provided",
		},
		"failure no image provided": {
			dir:         "/some/dir",
			namespace:   "namespace",
			imageRef:    "",
			expectedErr: "no image ref provided",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res, err := FilePath(tc.dir, tc.namespace, tc.imageRef)

			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				assert.Equal(t, tc.expectedRes, res)
			}
		})
	}
}
