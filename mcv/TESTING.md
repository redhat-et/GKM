# MCV Testing Documentation

This document provides an overview of the testing infrastructure for MCV (Model Cache Vault).

## Quick Start

```bash
# Run all unit tests
make test-short

# Run all tests with verbose output
make test

# Run tests with coverage report
make test-coverage

# Run end-to-end tests (requires built binary)
make test-e2e

# Run all tests (unit and e2e)
make test-all
```

## Test Structure

### Unit Tests

- [cmd/main_test.go](cmd/main_test.go) - CLI flag validation
- [pkg/imgbuild/builder_test.go](pkg/imgbuild/builder_test.go) - Builder selection logic
- [pkg/imgbuild/cosign_test.go](pkg/imgbuild/cosign_test.go) - Cosign Sign/Verify functions
- [pkg/imgbuild/utils_test.go](pkg/imgbuild/utils_test.go) - Dockerfile generation, cleanup
- [pkg/client/client_test.go](pkg/client/client_test.go) - Client API functions
- [pkg/config/config_test.go](pkg/config/config_test.go) - Configuration management
- [pkg/fetcher/fetcher_test.go](pkg/fetcher/fetcher_test.go) - Image fetching logic

**Run unit tests:**
```bash
make test
# or
go test ./... -v
```

### End-to-End (E2E) Tests

E2E tests verify the MCV CLI from an end-user perspective, testing the actual binary without complex dependencies.

**Location:** `test/e2e/mcv_e2e_test.go`

**Build tag:** `e2e`

**Test scenarios:**
1. `TestMCVVersionFlag` - Version command (`--version`)
2. `TestMCVHelpFlag` - Help command (`--help`)
3. `TestMCVGPUInfo` - GPU info with stub mode (`--gpu-info --stub`)
4. `TestMCVCheckCompat` - Compatibility check (`--check-compat`)
5. `TestMCVPushPullSignVerifyCLI` - Flag validation for push/pull/sign/verify and certificate identity constraints
6. `TestMCVSignVerifyUnreachableImage` - Sign/verify/push/pull fail past flag parsing on an unreachable image

**Run E2E tests:**
```bash
make test-e2e
# or
make build && cd test/e2e && go test -v -tags=e2e
```

## Test Coverage

### Coverage Reports

Generate HTML coverage report:
```bash
make test-coverage
```

View coverage in terminal:
```bash
go test ./... -cover
```

## Key Testing Features

### 1. Cosign Signature Testing

Tests for both keyless (Sigstore) and key-based signing:

**Unit tests ([pkg/imgbuild/cosign_test.go](pkg/imgbuild/cosign_test.go)):**
- Input validation (empty image references)
- Keyless vs key-based mode selection
- Error handling

### 2. Builder Selection Testing

Tests automatic detection and selection of buildah vs docker:

```go
// Test buildah available
func TestNew_BuildahAvailable(t *testing.T)

// Test docker fallback
func TestNew_DockerFallback(t *testing.T)

// Test unsupported builder
func TestNew_Unsupported(t *testing.T)
```

### 3. GPU Hardware Mocking

Tests support running without actual GPUs by using stub mode internally via the `EnableStub` configuration option in test code. See [pkg/client/client_test.go](pkg/client/client_test.go) for examples.

## Running Tests in Different Environments

### Local Development

```bash
# Quick feedback during development
make test-short

# Full local testing
make test
```

### CI/CD Pipeline

```bash
# Run in GitHub Actions, GitLab CI, etc.
make test              # Unit tests (always)
make test-e2e          # After building the binary
```

### Without GPU Hardware

All tests support running without actual GPU hardware - they use stub mode internally via the `EnableStub` configuration option, so no special flags are needed:

```bash
go test ./... -v
```

## Test Utilities and Helpers

### Table-Driven Tests

Most tests use table-driven patterns for comprehensive coverage:

```go
tests := []struct {
    name        string
    input       string
    expected    string
    expectError bool
}{
    {name: "Valid input", input: "test", expected: "result", expectError: false},
    {name: "Invalid input", input: "", expected: "", expectError: true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        result, err := Function(tt.input)
        if tt.expectError {
            assert.Error(t, err)
        } else {
            assert.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        }
    })
}
```

### Mock Functions

Tests use function variables for mocking external dependencies:

```go
// In production code
var HasApp = hasApp

// In test code
origHasApp := HasApp
defer func() { HasApp = origHasApp }()
HasApp = func(tool string) bool {
    return tool == "buildah"
}
```

### Temporary Directories

Tests use `t.TempDir()` for automatic cleanup:

```go
tmpDir := t.TempDir()  // Automatically cleaned up after test
```

## Debugging Tests

### Run Specific Test

```bash
go test ./pkg/imgbuild -v -run TestCosignSignAndVerify
```

Or run a specific subtest:

```bash
go test ./pkg/imgbuild -v -run TestCosignSignAndVerify/Sign/Empty_image_reference
```

### Run Tests in Package

```bash
go test ./pkg/imgbuild -v
```

### Enable Debug Logging

```bash
MCV_LOG_LEVEL=debug go test ./... -v
```

### Keep Test Artifacts

Modify tests to use a fixed directory instead of `t.TempDir()`:

```go
tmpDir := "/tmp/mcv-test-debug"
os.MkdirAll(tmpDir, 0755)
t.Logf("Test artifacts at: %s", tmpDir)
```

## Common Test Failures

### 1. Binary Not Found

**Error:**
```
Could not find mcv binary
```

**Solution:** Run `make build` to build the binary first

### 2. Test Cache Issues

**Error:**
```
Tests showing cached results
```

**Solution:** Clear test cache with `go clean -testcache`

## Adding New Tests

### 1. Add Unit Test

Create or update `*_test.go` file in the same package:

```go
func TestMyNewFunction(t *testing.T) {
    // Arrange
    input := "test"

    // Act
    result, err := MyNewFunction(input)

    // Assert
    assert.NoError(t, err)
    assert.Equal(t, "expected", result)
}
```

### 2. Add E2E Test

Add to [test/e2e/mcv_e2e_test.go](test/e2e/mcv_e2e_test.go):

```go
//go:build e2e

func TestMCVMyFeature(t *testing.T) {
    mcvBinary := findMCVBinary(t)

    cmd := exec.Command(mcvBinary, "--my-flag")
    output, err := cmd.CombinedOutput()

    assert.NoError(t, err)
    assert.Contains(t, string(output), "expected output")
}
```

## Performance Benchmarks

Run benchmarks:

```bash
go test ./... -bench=. -benchmem
```

Add benchmarks:

```go
func BenchmarkMyFunction(b *testing.B) {
    for i := 0; i < b.N; i++ {
        MyFunction("input")
    }
}
```

## Continuous Integration

### GitHub Actions Example

```yaml
name: Test

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Run unit tests
        run: make test
        working-directory: mcv

  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Build mcv
        run: make build
        working-directory: mcv
      - name: Run E2E tests
        run: make test-e2e
        working-directory: mcv
```

## Additional Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Testify Assertion Library](https://github.com/stretchr/testify)
- [Table-Driven Tests in Go](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [Test Directory README](test/README.md) - Detailed testing guide
