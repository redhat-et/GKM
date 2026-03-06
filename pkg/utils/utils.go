package utils

import (
	"flag"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func InitializeLogging(logLevel, component string, commandLine *flag.FlagSet) logr.Logger {
	var opts zap.Options

	// Setup logging
	switch strings.TrimSpace(logLevel) {
	case "info":
		opts = zap.Options{
			Development: false,
		}
	case "debug":
		opts = zap.Options{
			Development: true,
		}
	case "trace":
		opts = zap.Options{
			Development: true,
			Level:       zapcore.Level(-2),
		}
	default:
		// Default to Info
		opts = zap.Options{
			Development: false,
		}
	}

	if commandLine != nil {
		opts.BindFlags(commandLine)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	return ctrl.Log.WithName(component)
}

func ReplaceUrlTag(imageURL, digest string) string {
	// If invalid input, return empty string
	if imageURL == "" || digest == "" {
		return ""
	}

	// Check if the image already has a digest (e.g., from Kyverno mutation)
	// Format: registry/image:tag@sha256:digest
	if strings.Contains(imageURL, "@") {
		// Image already has a digest, check if it matches
		atIndex := strings.Index(imageURL, "@")
		existingDigest := imageURL[atIndex+1:]
		if existingDigest == digest {
			// Same digest, return as-is
			return imageURL
		}
		// Different digest, replace it
		return imageURL[:atIndex] + "@" + digest
	}

	// Tokenize the Image URL
	lastColonIndex := strings.LastIndex(imageURL, ":")
	if lastColonIndex == -1 {
		// No tag found, append the new tag
		return imageURL + "@" + digest
	}
	// Extract the part before the tag and append the new tag
	return imageURL[:lastColonIndex] + "@" + digest
}

func GenerateUniqueName(name string) string {
	uuid := uuid.New().String()
	return fmt.Sprintf("%s-%s", name, uuid[:8])
}
