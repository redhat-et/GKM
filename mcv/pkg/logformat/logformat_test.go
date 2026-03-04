package logformat

import (
	"testing"

	logging "github.com/sirupsen/logrus"
)

func TestConfigureLogging(t *testing.T) {
	tests := []struct {
		name      string
		logLevel  string
		expectErr bool
	}{
		{"Valid log level: info", "info", false},
		{"Valid log level: debug", "debug", false},
		{"Invalid log level", "invalid", true},
		{"Empty log level", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConfigureLogging(tt.logLevel)
			if (err != nil) != tt.expectErr {
				t.Errorf("ConfigureLogging(%q) error = %v, expectErr = %v", tt.logLevel, err, tt.expectErr)
			}

			if err == nil && tt.logLevel != "" {
				level, _ := logging.ParseLevel(tt.logLevel)
				if logging.GetLevel() != level {
					t.Errorf("Expected log level %v, got %v", level, logging.GetLevel())
				}
			}
		})
	}
}
