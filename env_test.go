package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvironmentDoesNotOverrideProcessEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("WEBTMUX_PORT=9090\nWEBTMUX_USERNAME=dotenv-user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"GOTTY_PORT": "7070"}
	lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }
	set := func(key, value string) error { env[key] = value; return nil }
	info, err := loadEnvironment([]string{"webtmux", "--env-file", path}, lookup, set)
	if err != nil {
		t.Fatal(err)
	}
	if !info.loaded || env["GOTTY_PORT"] != "7070" || env["WEBTMUX_PORT"] != "" {
		t.Fatalf("process environment was not preserved: %#v", env)
	}
	if env["WEBTMUX_USERNAME"] != "dotenv-user" {
		t.Fatalf("dotenv username was not loaded: %#v", env)
	}
}

func TestPrepareCredential(t *testing.T) {
	env := map[string]string{"WEBTMUX_USERNAME": "alice", "WEBTMUX_PASSWORD": "secret"}
	lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }
	set := func(key, value string) error { env[key] = value; return nil }
	if err := prepareCredential(lookup, set); err != nil {
		t.Fatal(err)
	}
	if env["WEBTMUX_CREDENTIAL"] != "alice:secret" {
		t.Fatalf("credential = %q", env["WEBTMUX_CREDENTIAL"])
	}
}
