package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/containers/buildah"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/redhat-et/GKM/mcv/pkg/client"
	"github.com/redhat-et/GKM/mcv/pkg/config"
	"github.com/redhat-et/GKM/mcv/pkg/imgbuild"
	"github.com/redhat-et/GKM/mcv/pkg/logformat"
	"github.com/redhat-et/GKM/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/storage/pkg/unshare"
)

const (
	exitNormal          = 0
	exitExtractError    = 1
	exitCreateError     = 2
	exitLogError        = 3
	exitPushError       = 4
	exitPullError       = 5
	exitSignError       = 6
	exitVerifyError     = 7
	exitGPUError        = 8
	exitValidationError = 9
	exitCompatError = 10
	version             = "1.0.0" // Application version
)

func main() {
	initializeLogging()

	if _, err := config.Initialize(config.ConfDir); err != nil {
		logFatal("Error initializing config", err, exitLogError)
	}

	if buildah.InitReexec() {
		return
	}
	unshare.MaybeReexecUsingUserNamespace(false)

	cmd := buildRootCommand()
	if err := cmd.Execute(); err != nil {
		logFatal("Error executing command", err, exitLogError)
	}
}

func initializeLogging() {
	logging.SetReportCaller(true)
	logging.SetFormatter(logformat.Default)
}

func logFatal(message string, err error, exitCode int) {
	logging.Errorf("%s: %v", message, err)
	os.Exit(exitCode)
}

type cliFlags struct {
	imageName    string
	cacheDirName string
	logLevel     string
	builder      string
	keyPath      string
	timeout      int

	create      bool
	extract     bool
	baremetal   bool
	noGPU       bool
	checkCompat bool
	gpuInfo     bool
	stub        bool
	sign        bool
	verify      bool
	push        bool
	pull        bool
	yes         bool

	// Cosign sign/verify options
	certIdentity         string
	certIdentityRegexp   string
	certOidcIssuer       string
	certOidcIssuerRegexp string
	ignoreTlog           bool
}

func buildRootCommand() *cobra.Command {
	var f cliFlags

	cmd := &cobra.Command{
		Use:   "mcv",
		Short: "A GPU Kernel runtime container image management utility",
		Long: `mcv is a utility for managing GPU kernel runtime container images.
It supports creating OCI images from cache directories, extracting caches from images,
pushing and pulling registry images, Cosign sign/verify, and hardware compatibility checks.`,
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if err := logformat.ConfigureLogging(f.logLevel); err != nil {
				logFatal("Error configuring logging", err, exitLogError)
			}
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := f.validate(); err != nil {
				logFatal("Error validating flags", err, exitValidationError)
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			handleRunCommand(f)
		},
	}

	addFlags(cmd, &f)
	return cmd
}

func addFlags(cmd *cobra.Command, f *cliFlags) {
	// Image operations
	cmd.Flags().StringVarP(&f.imageName, "image", "i", "", "OCI image name (required for create, extract, check-compat, push, pull, sign, verify)")
	cmd.Flags().StringVarP(&f.cacheDirName, "dir", "d", "", "Triton/vLLM cache directory path")

	// Actions (mutually exclusive main operations)
	cmd.Flags().BoolVarP(&f.create, "create", "c", false, "Create OCI image from cache directory")
	cmd.Flags().BoolVarP(&f.extract, "extract", "e", false, "Extract Triton/vLLM cache from OCI image")

	// Registry operations (mutually exclusive main operations)
	cmd.Flags().BoolVar(&f.push, "push", false, "Push image to registry")
	cmd.Flags().BoolVar(&f.pull, "pull", false, "Pull image from registry")

	// Information commands
	cmd.Flags().BoolVar(&f.gpuInfo, "gpu-info", false, "Display GPU-specific information")
	cmd.Flags().BoolVar(&f.checkCompat, "check-compat", false, "Check GPU compatibility with specified image")

	// Configuration options
	cmd.Flags().StringVarP(&f.logLevel, "log-level", "l", "info", "Set logging verbosity (debug, info, warning, error)")
	cmd.Flags().BoolVarP(&f.baremetal, "baremetal", "b", false, "Enable detailed baremetal preflight checks")
	cmd.Flags().BoolVar(&f.noGPU, "no-gpu", false, "Disable GPU detection and preflight checks (for testing)")
	cmd.Flags().BoolVar(&f.stub, "stub", false, "Use mock/stub data for hardware info (for testing)")
	cmd.Flags().StringVar(&f.builder, "builder", "", "Specify the builder to use (buildah or docker)")
	cmd.Flags().IntVarP(&f.timeout, "timeout", "t", 10, "Timeout in minutes for hardware detection operations (0 = disable timeout)")

	// Sign/Verify — standalone actions, or modifiers with --push / --pull
	cmd.Flags().BoolVarP(&f.sign, "sign", "s", false, "Sign the image in the registry (standalone or with --push)")
	cmd.Flags().BoolVar(&f.verify, "verify", false, "Verify the image signature (standalone or with --pull)")

	// Cosign-compatible signing/verification options (names match cosign CLI)
	cmd.Flags().StringVar(&f.keyPath, "key", "", "path to the private key file, KMS URI or Kubernetes Secret for signing/verification (use with --sign or --verify)")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "skip confirmation prompts for some irreversible actions (use with --sign)")
	cmd.Flags().StringVar(&f.certIdentity, "certificate-identity", "",
		"The identity expected in a valid Fulcio certificate. Valid values include email address, DNS names, IP addresses, and URIs. Set either --certificate-identity or --certificate-identity-regexp (not both) for keyless verify. If omitted with --verify (and no --key), any Fulcio identity is accepted.")
	cmd.Flags().StringVar(&f.certIdentityRegexp, "certificate-identity-regexp", "",
		"A regular expression alternative to --certificate-identity (mutually exclusive). Accepts the Go regular expression syntax described at https://golang.org/s/re2syntax.")
	cmd.Flags().StringVar(&f.certOidcIssuer, "certificate-oidc-issuer", "",
		"The OIDC issuer expected in a valid Fulcio certificate, e.g. https://token.actions.githubusercontent.com or https://oauth2.sigstore.dev/auth. Set either --certificate-oidc-issuer or --certificate-oidc-issuer-regexp (not both) for keyless verify. If omitted with --verify (and no --key), any Fulcio issuer is accepted.")
	cmd.Flags().StringVar(&f.certOidcIssuerRegexp, "certificate-oidc-issuer-regexp", "",
		"A regular expression alternative to --certificate-oidc-issuer (mutually exclusive). Accepts the Go regular expression syntax described at https://golang.org/s/re2syntax.")
	cmd.Flags().BoolVar(&f.ignoreTlog, "insecure-ignore-tlog", false,
		"ignore transparency log verification being unavailable / unsuccessful (use with --verify; keyless or key-based)")

	cmd.MarkFlagsOneRequired("create", "extract", "gpu-info", "check-compat", "push", "pull", "sign", "verify")
	cmd.MarkFlagsMutuallyExclusive("create", "extract", "gpu-info", "check-compat", "push", "pull")
	cmd.MarkFlagsMutuallyExclusive("no-gpu", "gpu-info")
	cmd.MarkFlagsMutuallyExclusive("no-gpu", "check-compat")
	cmd.MarkFlagsMutuallyExclusive("certificate-identity", "certificate-identity-regexp")
	cmd.MarkFlagsMutuallyExclusive("certificate-oidc-issuer", "certificate-oidc-issuer-regexp")
}

func handleRunCommand(f cliFlags) {
	switch {
	case f.create:
		runCreate(f.imageName, f.cacheDirName, f.builder)
	case f.push:
		runPush(f.imageName, f.sign, f.keyPath, f.yes, f.builder)
	case f.pull:
		runPull(f)
	case f.sign:
		runSign(f.imageName, f.keyPath, f.yes)
	case f.verify:
		runVerify(f)
	case f.gpuInfo:
		configureBoolFlags(f.baremetal, f.noGPU, f.stub)
		handleGPUInfo(f.timeout)
	case f.checkCompat:
		configureBoolFlags(f.baremetal, f.noGPU, f.stub)
		handleCheckCompat(f.imageName)
	case f.extract:
		configureBoolFlags(f.baremetal, f.noGPU, f.stub)
		runExtract(f.imageName, f.cacheDirName, f.logLevel, f.baremetal)
	}
}

func (f cliFlags) validate() error {
	primary := []bool{f.create, f.extract, f.gpuInfo, f.checkCompat, f.push, f.pull}
	primaryCount := 0
	for _, set := range primary {
		if set {
			primaryCount++
		}
	}
	if primaryCount > 1 {
		return fmt.Errorf("only one action flag can be specified at a time")
	}

	standaloneSign := f.sign && !f.push
	standaloneVerify := f.verify && !f.pull
	if primaryCount == 0 && !standaloneSign && !standaloneVerify {
		return fmt.Errorf("no action specified. Use --help to see available options")
	}
	if standaloneSign && standaloneVerify {
		return fmt.Errorf("only one action flag can be specified at a time")
	}

	if f.sign && (f.create || f.extract || f.gpuInfo || f.checkCompat || f.pull || f.verify) {
		return fmt.Errorf("--sign can only be used alone or with --push")
	}
	if f.verify && (f.create || f.extract || f.gpuInfo || f.checkCompat || f.push || f.sign) {
		return fmt.Errorf("--verify can only be used alone or with --pull")
	}

	if (f.create || f.extract || f.checkCompat || f.push || f.pull || f.sign || f.verify) && f.imageName == "" {
		return fmt.Errorf("--image is required when using --create, --extract, --check-compat, --push, --pull, --sign, or --verify")
	}
	if f.imageName != "" {
		if _, err := name.ParseReference(f.imageName, name.StrictValidation); err != nil {
			return fmt.Errorf("error validating image name: %v", err)
		}
	}

	if f.create && f.cacheDirName == "" {
		return fmt.Errorf("--dir is required when using --create")
	}
	if f.stub && !f.gpuInfo {
		return fmt.Errorf("--stub can only be used with --gpu-info")
	}
	if f.yes && !f.sign {
		return fmt.Errorf("--yes can only be used with --sign")
	}

	if f.keyPath != "" {
		if !f.sign && !f.verify {
			return fmt.Errorf("--key can only be used with --sign or --verify")
		}
		// Validate key path exists (only for file paths, not URIs).
		// URIs include: aws://, gcpkms://, azurekms://, vault://, pkcs11:, k8s://, github://, gitlab://
		if !strings.Contains(f.keyPath, "://") && !strings.HasPrefix(f.keyPath, "pkcs11:") {
			if _, err := os.Stat(f.keyPath); err != nil {
				return fmt.Errorf("key file not found: %s", f.keyPath)
			}
		}
	}

	if f.certIdentity != "" && f.certIdentityRegexp != "" {
		return fmt.Errorf("--certificate-identity and --certificate-identity-regexp are mutually exclusive")
	}
	if f.certOidcIssuer != "" && f.certOidcIssuerRegexp != "" {
		return fmt.Errorf("--certificate-oidc-issuer and --certificate-oidc-issuer-regexp are mutually exclusive")
	}

	hasIdentity := f.certIdentity != "" || f.certIdentityRegexp != ""
	hasIssuer := f.certOidcIssuer != "" || f.certOidcIssuerRegexp != ""
	if hasIdentity || hasIssuer {
		if !f.verify {
			return fmt.Errorf("certificate identity flags can only be used with --verify")
		}
		if f.keyPath != "" {
			return fmt.Errorf("certificate identity flags cannot be used with --key")
		}
		if !hasIdentity || !hasIssuer {
			return fmt.Errorf("keyless verification with identity constraints requires (--certificate-identity or --certificate-identity-regexp) and (--certificate-oidc-issuer or --certificate-oidc-issuer-regexp)")
		}
	}
	if f.ignoreTlog && !f.verify {
		return fmt.Errorf("--insecure-ignore-tlog can only be used with --verify")
	}

	return nil
}

// handleGPUInfo retrieves and displays GPU information for the system.
func handleGPUInfo(timeout int) {
	stub := config.IsStubEnabled()
	summary, err := client.GetSystemGPUInfo(client.HwOptions{EnableStub: &stub, Timeout: timeout})
	if err != nil && summary == nil {
		logFatal("Error getting system hardware", err, exitGPUError)
	}
	client.PrintGPUSummary(summary)

	os.Exit(exitNormal)
}

// handleCheckCompat checks GPU compatibility between the system and the specified image.
func handleCheckCompat(imageName string) {
	matched, unmatched, err := client.PreflightCheck(imageName)
	if err != nil {
		logging.Errorf("Preflight check failed: %v", err)
	}

	if len(matched) > 0 {
		logging.Debugf("Compatible GPU(s) found (%d):", len(matched))
		logging.Debugf("IDs: %v", matched)
	} else {
		logging.Warn("No compatible GPUs found for the image.")
	}

	if len(unmatched) > 0 {
		logging.Debugf("Incompatible GPU(s) found (%d):", len(unmatched))
		logging.Debugf("IDs: %v", unmatched)
	}

	if err != nil || len(matched) == 0 {
		logging.Warn("Exiting: no compatible GPU(s) detected or error occurred during compatibility check")
		os.Exit(exitCompatError)
	}
	os.Exit(exitNormal)
}

// configureBoolFlags sets global configuration flags for baremetal, GPU, and stub modes.
func configureBoolFlags(baremetalFlag, noGPUFlag, stub bool) {
	config.SetEnabledBaremetal(baremetalFlag)
	config.SetEnabledStub(stub)
	config.SetEnabledGPU(!noGPUFlag)

	logging.Debugf("baremetalFlag %v", baremetalFlag)
	logging.Debugf("stub %v", stub)
	logging.Debugf("noGPUFlag %v", noGPUFlag)

	if noGPUFlag {
		logging.Debug("GPU checks disabled: running in no-GPU mode (--no-gpu)")
		return
	}
}

// runCreate creates an OCI image from a local cache directory.
func runCreate(imageName, cacheDir, builder string) {
	// Check if the cache directory exists
	if _, err := utils.FilePathExists(cacheDir); err != nil {
		logFatal("Error checking cache file path", err, exitCreateError)
	}

	builderInstance, err := imgbuild.NewWithBuilder(builder)
	if err != nil {
		logFatal("Failed to create builder", err, exitCreateError)
	}

	// Create the OCI image
	if err := builderInstance.CreateImage(imageName, cacheDir); err != nil {
		logFatal("Failed to create the OCI image", err, exitCreateError)
	}

	logging.Info("OCI image created successfully.")
}

// runExtract extracts the cache from an OCI image to a local directory.
func runExtract(imageName, cacheDir, logLevel string, baremetalFlag bool) {
	// Ensure cache directory exists if specified
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			logFatal("Failed to create cache directory", err, exitExtractError)
		}
	}

	gpuEnabled := config.IsGPUEnabled()
	opts := client.Options{
		ImageName:       imageName,
		CacheDir:        cacheDir,
		EnableGPU:       &gpuEnabled,
		LogLevel:        logLevel,
		EnableBaremetal: &baremetalFlag,
	}
	if _, _, err := client.ExtractCache(opts); err != nil {
		logFatal("Error extracting image", err, exitExtractError)
	}
}

// runPush pushes an OCI image to a registry, optionally signing it after the push.
func runPush(imageName string, signFlag bool, keyPath string, yesFlag bool, builder string) {
	builderInstance, err := imgbuild.NewWithBuilder(builder)
	if err != nil {
		logFatal("Failed to create builder", err, exitPushError)
	}

	pushedDigestRef, err := builderInstance.PushImage(imageName)
	if err != nil {
		logFatal("Failed to push the OCI image", err, exitPushError)
	}

	logging.Info("OCI image pushed to registry successfully.")

	if signFlag {
		digestRef := pushedDigestRef
		if digestRef == "" {
			var resolveErr error
			digestRef, resolveErr = imgbuild.ResolveRegistryDigest(imageName)
			if resolveErr != nil || digestRef == "" {
				if resolveErr != nil {
					logging.Errorf("image was pushed but digest could not be resolved from registry: %v", resolveErr)
				} else {
					logging.Errorf("image was pushed but registry did not report a digest for signing")
				}
				logging.Errorf("Refusing to sign a mutable tag for --push --sign; re-run with --sign on an explicit digest")
				os.Exit(exitSignError)
			}
			logging.Infof("Resolved pushed image digest from registry: %s", digestRef)
		}
		if err := signDigestRef(digestRef, keyPath, yesFlag); err != nil {
			logFatal("Image was pushed but not signed; re-run with --sign after fixing access, or sign manually", err, exitSignError)
		}
	}
}

// runPull pulls an OCI image from a registry, optionally verifying its signature first.
func runPull(f cliFlags) {
	pullRef := f.imageName

	if f.verify {
		digestRef, err := verifyImage(f)
		if err != nil {
			logFatal("Image verification failed", err, exitVerifyError)
		}
		pullRef = digestRef
	}

	builderInstance, err := imgbuild.NewWithBuilder(f.builder)
	if err != nil {
		logFatal("Failed to create builder", err, exitPullError)
	}

	if err := builderInstance.PullImage(pullRef); err != nil {
		logFatal("Failed to pull the OCI image", err, exitPullError)
	}

	logging.Info("OCI image pulled from registry successfully.")
}

// runSign signs an OCI image using Cosign.
func runSign(imageName, keyPath string, yesFlag bool) {
	if err := signImage(imageName, keyPath, yesFlag); err != nil {
		logFatal("Image signing failed", err, exitSignError)
	}
}

// runVerify verifies an OCI image signature using Cosign.
func runVerify(f cliFlags) {
	if _, err := verifyImage(f); err != nil {
		logFatal("Image verification failed", err, exitVerifyError)
	}
}

// signImage resolves an image name to its digest and signs it.
func signImage(imageName, keyPath string, yesFlag bool) error {
	digestRef, err := imgbuild.ResolveDigest(imageName)
	if err != nil {
		return fmt.Errorf("failed to resolve image digest for signing: %w", err)
	}
	return signDigestRef(digestRef, keyPath, yesFlag)
}

// signDigestRef signs an image by its digest reference using Cosign.
func signDigestRef(digestRef, keyPath string, yesFlag bool) error {
	logging.Infof("Signing image digest: %s", digestRef)

	if err := imgbuild.Sign(digestRef, keyPath, yesFlag); err != nil {
		return fmt.Errorf("failed to sign the OCI image: %w", err)
	}
	logging.Info("OCI image signed successfully.")
	return nil
}

// verifyImage resolves and verifies an image signature using Cosign, returning the digest reference.
func verifyImage(f cliFlags) (string, error) {
	digestRef, err := imgbuild.ResolveRegistryDigest(f.imageName)
	if err != nil {
		return "", fmt.Errorf("failed to resolve image digest: %w", err)
	}
	logging.Infof("Resolved image to digest: %s", digestRef)

	if err := imgbuild.Verify(digestRef, imgbuild.VerifyOptions{
		KeyPath:              f.keyPath,
		CertIdentity:         f.certIdentity,
		CertIdentityRegexp:   f.certIdentityRegexp,
		CertOidcIssuer:       f.certOidcIssuer,
		CertOidcIssuerRegexp: f.certOidcIssuerRegexp,
		IgnoreTlog:           f.ignoreTlog,
	}); err != nil {
		return "", fmt.Errorf("failed to verify the OCI image signature: %w", err)
	}
	logging.Info("OCI image signature verified successfully.")
	return digestRef, nil
}
