package server

import (
	"os"
	"path/filepath"
	"testing"

	"webtmux/utils"
)

func TestLaunchctlDefaultsOffAndCanBeEnabledByConfig(t *testing.T) {
	options := &Options{}
	if err := utils.ApplyDefaultValues(options); err != nil {
		t.Fatal(err)
	}
	if options.Launchctl {
		t.Fatal("launchctl must be opt-in")
	}

	path := filepath.Join(t.TempDir(), "config.hcl")
	if err := os.WriteFile(path, []byte("launchctl = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := utils.ApplyConfigFile(path, options); err != nil {
		t.Fatal(err)
	}
	if !options.Launchctl {
		t.Fatal("launchctl config was not applied")
	}
}

func TestValidateLoggingOptions(t *testing.T) {
	options := &Options{}
	if err := utils.ApplyDefaultValues(options); err != nil {
		t.Fatal(err)
	}
	if err := options.Validate(); err != nil {
		t.Fatalf("default logging options are invalid: %v", err)
	}
	options.LogLevel = "warning"
	if err := options.Validate(); err != nil || options.LogLevel != "warn" {
		t.Fatalf("warning alias was not normalized: level=%q err=%v", options.LogLevel, err)
	}
	options.LogLevel = "trace"
	if err := options.Validate(); err == nil {
		t.Fatal("unsupported log level was accepted")
	}
	options.LogLevel = "debug"
	options.LogFormat = "xml"
	if err := options.Validate(); err == nil {
		t.Fatal("unsupported log format was accepted")
	}
}
