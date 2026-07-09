# MCV Testing Guide

This directory contains various test suites for MCV (Model Cache Vault).

## Test Types

### Unit Tests
Unit tests are located alongside the source code in `*_test.go` files and test individual functions and components in isolation.

**Run all unit tests:**
```bash
cd mcv
go test ./... -v
```

**Run unit tests for a specific package:**
```bash
go test ./pkg/imgbuild -v
go test ./pkg/client -v
go test ./pkg/config -v
```

**Run specific unit tests:**
```bash
go test ./pkg/imgbuild -v -run TestSign
go test ./pkg/imgbuild -v -run TestVerify
```

### End-to-End (E2E) Tests
E2E tests verify the MCV CLI from an end-user perspective:
- `TestMCVVersionFlag` - Version command (`--version`)
- `TestMCVHelpFlag` - Help command (`--help`)
- `TestMCVGPUInfo` - GPU info with stub mode (`--gpu-info --stub`)
- `TestMCVCheckCompat` - Compatibility checking (`--check-compat`)
- `TestMCVPushPullSignVerifyCLI` - Push/pull/sign/verify flag validation and certificate constraints
- `TestMCVSignVerifyUnreachableImage` - Push/pull/sign/verify wired past flag parsing

E2E tests are **simplified and focused** - each test function tests one specific CLI feature without requiring complex cache setups or registries.

**Run E2E tests:**
```bash
make test-e2e
# or
cd mcv
make build  # Build the mcv binary first
cd test/e2e
go test -v -tags=e2e
```

**Prerequisites for E2E tests:**
1. MCV binary built (`make build`)

## Test Coverage

### Current Test Coverage

#### pkg/imgbuild

- ✅ Builder initialization (buildah/docker selection)
- ✅ Cosign Sign function (input validation, keyless/key-based modes)
- ✅ Cosign Verify function (input validation, keyless/key-based modes)
- ✅ Dockerfile generation
- ✅ Directory cleanup

#### pkg/client

- ✅ ExtractCache with GPU enabled/disabled
- ✅ GPU info retrieval
- ✅ Preflight checks
- ✅ Cache inspection
- ✅ Custom cache directory support

#### pkg/config

- ✅ Configuration initialization
- ✅ Environment variable overrides
- ✅ Setters and getters

#### cmd/main

- ✅ Flag combination validation
- ✅ Image name validation
- ✅ E2E: CLI commands (version, help, gpu-info, check-compat, push/pull/sign/verify)

### Getting Test Coverage Reports

**Generate coverage report:**
```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

**View coverage in terminal:**
```bash
go test ./... -cover
```

## Running Tests in CI/CD

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
        run: |
          cd mcv
          go test ./... -v

  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Build mcv
        run: |
          cd mcv
          make build
      - name: Run E2E tests
        run: |
          cd mcv/test/e2e
          go test -v -tags=e2e
```

## Test Utilities

### Mock Data
Tests use mock/stub data for GPU hardware information when running in environments without actual GPUs. This is handled automatically via the `EnableStub` configuration option in test code - no special flags needed.

## Debugging Tests

### Verbose Output
```bash
go test ./... -v
```

### Run Specific Test
```bash
go test ./pkg/imgbuild -v -run TestSign_EmptyImageRef
```

### Keep Test Artifacts
By default, tests use `t.TempDir()` which auto-cleans up. To debug, modify tests to use a fixed directory:

```go
// Instead of:
tmpDir := t.TempDir()

// Use:
tmpDir := "/tmp/mcv-test-debug"
os.MkdirAll(tmpDir, 0755)
t.Logf("Test artifacts at: %s", tmpDir)
```

### Enable Debug Logging
```bash
MCV_LOG_LEVEL=debug go test ./... -v
```

## Adding New Tests

### Unit Test Template

```go
func TestMyNewFunction(t *testing.T) {
    tests := []struct {
        name        string
        input       string
        expected    string
        expectError bool
    }{
        {
            name:        "Valid input",
            input:       "test",
            expected:    "expected",
            expectError: false,
        },
        // Add more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := MyNewFunction(tt.input)
            if tt.expectError {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expected, result)
            }
        })
    }
}
```

### E2E Test Template

```go
// +build e2e

func TestMCVMyFeature(t *testing.T) {
    mcvBinary := findMCVBinary(t)
    
    cmd := exec.Command(mcvBinary, "--my-flag")
    output, err := cmd.CombinedOutput()
    
    assert.NoError(t, err)
    assert.Contains(t, string(output), "expected output")
}
```

## Common Issues

### Binary Not Found
```
Error: Could not find mcv binary
```
**Solution:** Run `make build` to build the binary first

### Test Cache Issues
```
Tests showing cached results
```
**Solution:** Clear test cache with `go clean -testcache`

### Permission Denied (Buildah)
```
Error: cannot set up namespace using newuidmap
```
**Solution:** Run with appropriate permissions or use Docker instead

## Performance Benchmarks

Run performance benchmarks:

```bash
go test ./... -bench=. -benchmem
```
