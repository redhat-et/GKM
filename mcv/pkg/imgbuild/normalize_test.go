package imgbuild

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeImageTag(t *testing.T) {
	digestRef := "quay.io/gkm/cache-examples@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		in, want string
	}{
		{"example.com/repo/image", "example.com/repo/image:latest"},
		{"example.com/repo/image:v1", "example.com/repo/image:v1"},
		{digestRef, digestRef},
		{"localhost:5000/foo", "localhost:5000/foo:latest"},
		{"localhost:5000/foo:v1", "localhost:5000/foo:v1"},
		{"foo", "foo:latest"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeImageTag(tt.in))
		})
	}
}
