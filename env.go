package main

import (
	"fmt"
	"os"
	"strings"

	"webtmux/pkg/dotenv"
)

type environmentInfo struct {
	path       string
	loaded     bool
	permission os.FileMode
}

func loadEnvironment(args []string, lookup func(string) (string, bool), set func(string, string) error) (environmentInfo, error) {
	path, explicit := envFilePath(args, lookup)
	values, permission, err := dotenv.Read(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return environmentInfo{path: path}, nil
		}
		return environmentInfo{}, err
	}
	for key, value := range values {
		if environmentKeyAlreadySet(key, lookup) {
			continue
		}
		if err := set(key, value); err != nil {
			return environmentInfo{}, fmt.Errorf("set %s from %s: %w", key, path, err)
		}
	}
	return environmentInfo{path: path, loaded: true, permission: permission}, nil
}

func envFilePath(args []string, lookup func(string) (string, bool)) (string, bool) {
	for index := 1; index < len(args); index++ {
		if args[index] == "--env-file" && index+1 < len(args) {
			return args[index+1], true
		}
		if strings.HasPrefix(args[index], "--env-file=") {
			return strings.TrimPrefix(args[index], "--env-file="), true
		}
	}
	if path, ok := lookup("WEBTMUX_ENV_FILE"); ok && path != "" {
		return path, true
	}
	return ".env", false
}

// Treat old GOTTY_* and new WEBTMUX_* names as the same setting so a value in
// .env can never shadow an existing process environment variable through an
// alias.
func environmentKeyAlreadySet(key string, lookup func(string) (string, bool)) bool {
	if _, ok := lookup(key); ok {
		return true
	}
	var alias string
	if strings.HasPrefix(key, "WEBTMUX_") {
		alias = "GOTTY_" + strings.TrimPrefix(key, "WEBTMUX_")
	} else if strings.HasPrefix(key, "GOTTY_") {
		alias = "WEBTMUX_" + strings.TrimPrefix(key, "GOTTY_")
	}
	_, ok := lookup(alias)
	return alias != "" && ok
}

func prepareCredential(lookup func(string) (string, bool), set func(string, string) error) error {
	if _, ok := lookup("WEBTMUX_CREDENTIAL"); ok {
		return nil
	}
	if _, ok := lookup("GOTTY_CREDENTIAL"); ok {
		return nil
	}
	password, ok := lookup("WEBTMUX_PASSWORD")
	if !ok || password == "" {
		return nil
	}
	username, ok := lookup("WEBTMUX_USERNAME")
	if !ok || username == "" {
		username = "admin"
	}
	return set("WEBTMUX_CREDENTIAL", username+":"+password)
}

func configuredUsername(lookup func(string) (string, bool)) string {
	if username, ok := lookup("WEBTMUX_USERNAME"); ok && username != "" {
		return username
	}
	return "admin"
}
