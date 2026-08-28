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
