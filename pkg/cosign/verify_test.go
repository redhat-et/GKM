package cosign

import (
	"context"
	"testing"
	"time"
)

func TestVerifyImageSignature(t *testing.T) {
	tests := []struct {
		name      string
		imageRef  string
		wantError bool
		skipCI    bool // Skip in CI environments without registry access
	}{
		{
			name:      "Cosign v2 signed image (legacy .sig tag)",
			imageRef:  "quay.io/gkm/cache-examples:vector-add-cache-rocm",
			wantError: false,
			skipCI:    true,
		},
		{
			name:      "Cosign v3 signed image (OCI 1.1 bundle)",
			imageRef:  "quay.io/mtahhan/vllm-flash-attention:rocm",
			wantError: false,
			skipCI:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipCI && testing.Short() {
				t.Skip("Skipping test in short mode (use -short=false to run)")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			digest, err := VerifyImageSignature(ctx, tt.imageRef)
			if (err != nil) != tt.wantError {
				t.Logf("Full error details: %+v", err)
				t.Errorf("VerifyImageSignature() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError {
				if digest == "" {
					t.Errorf("VerifyImageSignature() returned empty digest")
				}
				t.Logf("Successfully verified image: %s", tt.imageRef)
				t.Logf("Digest: %s", digest)
			}
		})
	}
}
