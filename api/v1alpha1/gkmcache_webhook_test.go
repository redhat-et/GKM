/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"os"
	"testing"

	"github.com/redhat-et/GKM/pkg/utils"
)

func TestVerifyKyvernoAnnotation(t *testing.T) {
	tests := []struct {
		name           string
		annotations    map[string]string
		expectedDigest string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "no kyverno annotation present - should pass",
			annotations:    map[string]string{},
			expectedDigest: "sha256:abc123",
			wantErr:        false,
		},
		{
			name: "valid kyverno annotation with pass status",
			annotations: map[string]string{
				"kyverno.io/verify-images": `{"quay.io/gkm/cache-examples@sha256:abc123":"pass"}`,
			},
			expectedDigest: "sha256:abc123",
			wantErr:        false,
		},
		{
			name: "kyverno annotation with fail status",
			annotations: map[string]string{
				"kyverno.io/verify-images": `{"quay.io/gkm/cache-examples@sha256:abc123":"fail"}`,
			},
			expectedDigest: "sha256:abc123",
			wantErr:        true,
			errContains:    "not 'pass'",
		},
		{
			name: "kyverno annotation with mismatched digest",
			annotations: map[string]string{
				"kyverno.io/verify-images": `{"quay.io/gkm/cache-examples@sha256:different":"pass"}`,
			},
			expectedDigest: "sha256:abc123",
			wantErr:        true,
			errContains:    "does not match expected digest",
		},
		{
			name: "invalid JSON in kyverno annotation",
			annotations: map[string]string{
				"kyverno.io/verify-images": `not valid json`,
			},
			expectedDigest: "sha256:abc123",
			wantErr:        true,
			errContains:    "failed to parse",
		},
		{
			name: "kyverno annotation with empty status",
			annotations: map[string]string{
				"kyverno.io/verify-images": `{"quay.io/gkm/cache-examples@sha256:abc123":""}`,
			},
			expectedDigest: "sha256:abc123",
			wantErr:        true,
			errContains:    "not 'pass'",
		},
		{
			name: "kyverno annotation with pending status",
			annotations: map[string]string{
				"kyverno.io/verify-images": `{"quay.io/gkm/cache-examples@sha256:abc123":"pending"}`,
			},
			expectedDigest: "sha256:abc123",
			wantErr:        true,
			errContains:    "not 'pass'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyKyvernoAnnotation(tt.annotations, tt.expectedDigest)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyKyvernoAnnotation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if err.Error() == "" || !containsString(err.Error(), tt.errContains) {
					t.Errorf("verifyKyvernoAnnotation() error = %v, should contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestIsKyvernoVerificationEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{
			name:     "env not set - defaults to true",
			envValue: "",
			want:     true,
		},
		{
			name:     "env set to true",
			envValue: "true",
			want:     true,
		},
		{
			name:     "env set to TRUE (case insensitive)",
			envValue: "TRUE",
			want:     true,
		},
		{
			name:     "env set to 1",
			envValue: "1",
			want:     true,
		},
		{
			name:     "env set to yes",
			envValue: "yes",
			want:     true,
		},
		{
			name:     "env set to YES (case insensitive)",
			envValue: "YES",
			want:     true,
		},
		{
			name:     "env set to false",
			envValue: "false",
			want:     false,
		},
		{
			name:     "env set to FALSE (case insensitive)",
			envValue: "FALSE",
			want:     false,
		},
		{
			name:     "env set to 0",
			envValue: "0",
			want:     false,
		},
		{
			name:     "env set to no",
			envValue: "no",
			want:     false,
		},
		{
			name:     "env set to NO (case insensitive)",
			envValue: "NO",
			want:     false,
		},
		{
			name:     "invalid value - defaults to true",
			envValue: "invalid",
			want:     true,
		},
		{
			name:     "random string - defaults to true",
			envValue: "random",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env value
			originalEnv := os.Getenv(utils.EnvKyvernoEnabled)
			defer func() {
				// Restore original env value
				if originalEnv != "" {
					os.Setenv(utils.EnvKyvernoEnabled, originalEnv)
				} else {
					os.Unsetenv(utils.EnvKyvernoEnabled)
				}
			}()

			// Set test env value
			if tt.envValue != "" {
				os.Setenv(utils.EnvKyvernoEnabled, tt.envValue)
			} else {
				os.Unsetenv(utils.EnvKyvernoEnabled)
			}

			got := isKyvernoVerificationEnabled()
			if got != tt.want {
				t.Errorf("isKyvernoVerificationEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
