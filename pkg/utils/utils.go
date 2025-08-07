package utils

import (
	"flag"
	"strings"

	"github.com/go-logr/logr"
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
