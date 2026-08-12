package main

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testImageName       = "quay.io/gkm/cache-examples:vector-add-cache-cuda"
	testCacheDirName    = "../example/vector-add-cache"
	testGitHubIssuer    = "https://token.actions.githubusercontent.com"
	testCertIdentity    = "user@example.com"
)

func TestValidateFlagCombinations(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "testkey.pem")
	if err := os.WriteFile(keyFile, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("failed to create temp key: %v", err)
	}

	tests := []struct {
		name        string
		flags       cliFlags
		expectError bool
	}{
		{
			name: "Valid create flag with image and dir",
			flags: cliFlags{
				create:       true,
				imageName:    testImageName,
				cacheDirName: testCacheDirName,
			},
		},
		{
			name: "Missing image name for create",
			flags: cliFlags{
				create:       true,
				cacheDirName: testCacheDirName,
			},
			expectError: true,
		},
		{
			name: "Multiple action flags",
			flags: cliFlags{
				create:       true,
				extract:      true,
				imageName:    testImageName,
				cacheDirName: testCacheDirName,
			},
			expectError: true,
		},
		{
			name: "Invalid image name format",
			flags: cliFlags{
				create:       true,
				imageName:    "invalid:image_name",
				cacheDirName: testCacheDirName,
			},
			expectError: true,
		},
		{
			name:        "Stub flag without gpu-info",
			flags:       cliFlags{stub: true},
			expectError: true,
		},
		{
			name: "Valid check-compat flag with image",
			flags: cliFlags{
				checkCompat: true,
				imageName:   testImageName,
			},
		},
		{
			name: "Valid push with sign and key",
			flags: cliFlags{
				push:      true,
				sign:      true,
				imageName: testImageName,
				keyPath:   keyFile,
			},
		},
		{
			name: "Push with sign and missing key file",
			flags: cliFlags{
				push:      true,
				sign:      true,
				imageName: testImageName,
				keyPath:   "testkey.pem",
			},
			expectError: true,
		},
		{
			name: "Key without push/sign or pull/verify",
			flags: cliFlags{
				create:       true,
				imageName:    testImageName,
				cacheDirName: testCacheDirName,
				keyPath:      keyFile,
			},
			expectError: true,
		},
		{
			name: "Yes flag without sign",
			flags: cliFlags{
				create:       true,
				imageName:    testImageName,
				cacheDirName: testCacheDirName,
				yes:          true,
			},
			expectError: true,
		},
		{
			name: "Sign without push or alone (with pull)",
			flags: cliFlags{
				sign:      true,
				pull:      true,
				imageName: testImageName,
			},
			expectError: true,
		},
		{
			name: "Verify without pull or alone (with push)",
			flags: cliFlags{
				verify:    true,
				push:      true,
				imageName: testImageName,
			},
			expectError: true,
		},
		{
			name: "Standalone sign",
			flags: cliFlags{
				sign:      true,
				imageName: testImageName,
			},
		},
		{
			name: "Standalone sign with key",
			flags: cliFlags{
				sign:      true,
				imageName: testImageName,
				keyPath:   keyFile,
			},
		},
		{
			name: "Standalone sign with yes",
			flags: cliFlags{
				sign:      true,
				yes:       true,
				imageName: testImageName,
			},
		},
		{
			name: "Standalone verify",
			flags: cliFlags{
				verify:    true,
				imageName: testImageName,
			},
		},
		{
			name: "Standalone verify with key",
			flags: cliFlags{
				verify:    true,
				imageName: testImageName,
				keyPath:   keyFile,
			},
		},
		{
			name: "Standalone keyless verify with identity constraints",
			flags: cliFlags{
				verify:         true,
				imageName:      testImageName,
				certIdentity:   "https://github.com/org/repo/.github/workflows/ci.yml@refs/heads/main",
				certOidcIssuer: testGitHubIssuer,
			},
		},
		{
			name:        "Standalone sign missing image",
			flags:       cliFlags{sign: true},
			expectError: true,
		},
		{
			name:        "Standalone verify missing image",
			flags:       cliFlags{verify: true},
			expectError: true,
		},
		{
			name: "Sign and verify together",
			flags: cliFlags{
				sign:      true,
				verify:    true,
				imageName: testImageName,
			},
			expectError: true,
		},
		{
			name: "Keyless verify without identity constraints",
			flags: cliFlags{
				pull:      true,
				verify:    true,
				imageName: testImageName,
			},
		},
		{
			name: "Keyless verify with certificate-identity and certificate-oidc-issuer",
			flags: cliFlags{
				pull:           true,
				verify:         true,
				imageName:      testImageName,
				certIdentity:   "https://github.com/org/repo/.github/workflows/ci.yml@refs/heads/main",
				certOidcIssuer: testGitHubIssuer,
			},
		},
		{
			name: "Keyless verify with certificate-identity-regexp and certificate-oidc-issuer-regexp",
			flags: cliFlags{
				pull:                 true,
				verify:               true,
				imageName:            testImageName,
				certIdentityRegexp:   ".*@example.com",
				certOidcIssuerRegexp: "https://.*\\.example\\.com",
			},
		},
		{
			name: "Keyless verify with only certificate-identity",
			flags: cliFlags{
				pull:         true,
				verify:       true,
				imageName:    testImageName,
				certIdentity: testCertIdentity,
			},
			expectError: true,
		},
		{
			name: "Certificate identity with key",
			flags: cliFlags{
				pull:           true,
				verify:         true,
				imageName:      testImageName,
				keyPath:        keyFile,
				certIdentity:   testCertIdentity,
				certOidcIssuer: testGitHubIssuer,
			},
			expectError: true,
		},
		{
			name: "Keyless verify with ignore tlog",
			flags: cliFlags{
				pull:       true,
				verify:     true,
				imageName:  testImageName,
				ignoreTlog: true,
			},
		},
		{
			name: "Key-based verify with ignore tlog",
			flags: cliFlags{
				pull:       true,
				verify:     true,
				imageName:  testImageName,
				keyPath:    keyFile,
				ignoreTlog: true,
			},
		},
		{
			name: "Standalone keyless verify with ignore tlog",
			flags: cliFlags{
				verify:     true,
				imageName:  testImageName,
				ignoreTlog: true,
			},
		},
		{
			name: "Key URI does not require local file",
			flags: cliFlags{
				push:      true,
				sign:      true,
				imageName: testImageName,
				keyPath:   "awskms://alias/cosign-key",
			},
		},
		{
			name: "PKCS11 key URI does not require local file",
			flags: cliFlags{
				push:      true,
				sign:      true,
				imageName: testImageName,
				keyPath:   "pkcs11:object=cosign-key",
			},
		},
		{
			name: "Valid push without sign",
			flags: cliFlags{
				push:      true,
				imageName: testImageName,
			},
		},
		{
			name: "Valid pull without verify",
			flags: cliFlags{
				pull:      true,
				imageName: testImageName,
			},
		},
		{
			name:        "Push missing image name",
			flags:       cliFlags{push: true},
			expectError: true,
		},
		{
			name:        "Pull missing image name",
			flags:       cliFlags{pull: true},
			expectError: true,
		},
		{
			name: "Certificate flags without verify",
			flags: cliFlags{
				pull:           true,
				imageName:      testImageName,
				certIdentity:   testCertIdentity,
				certOidcIssuer: testGitHubIssuer,
			},
			expectError: true,
		},
		{
			name: "Mixed identity exact and issuer regexp",
			flags: cliFlags{
				pull:                 true,
				verify:               true,
				imageName:            testImageName,
				certIdentity:         testCertIdentity,
				certOidcIssuerRegexp: "https://.*",
			},
		},
		{
			name: "Both certificate-identity and certificate-identity-regexp",
			flags: cliFlags{
				pull:               true,
				verify:             true,
				imageName:          testImageName,
				certIdentity:       testCertIdentity,
				certIdentityRegexp: ".*@example.com",
				certOidcIssuer:     "https://token.actions.githubusercontent.com",
			},
			expectError: true,
		},
		{
			name: "Both certificate-oidc-issuer and certificate-oidc-issuer-regexp",
			flags: cliFlags{
				pull:                 true,
				verify:               true,
				imageName:            testImageName,
				certIdentity:         testCertIdentity,
				certOidcIssuer:       "https://token.actions.githubusercontent.com",
				certOidcIssuerRegexp: "https://.*",
			},
			expectError: true,
		},
		{
			name: "Only certificate-oidc-issuer",
			flags: cliFlags{
				pull:           true,
				verify:         true,
				imageName:      testImageName,
				certOidcIssuer: testGitHubIssuer,
			},
			expectError: true,
		},
		{
			name: "Ignore tlog without verify",
			flags: cliFlags{
				pull:       true,
				imageName:  testImageName,
				keyPath:    keyFile,
				ignoreTlog: true,
			},
			expectError: true,
		},
		{
			name: "Yes with push and sign",
			flags: cliFlags{
				push:      true,
				sign:      true,
				yes:       true,
				imageName: testImageName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flags.validate()
			if (err != nil) != tt.expectError {
				t.Errorf("Expected error: %v, got: %v", tt.expectError, err)
			}
		})
	}
}
