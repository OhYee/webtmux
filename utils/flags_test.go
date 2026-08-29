package utils

import (
	"testing"

	"github.com/urfave/cli/v2"
)

type priorityOptions struct {
	Port string `flagName:"port" default:"8080"`
}

func runPriorityApp(t *testing.T, args []string, options *priorityOptions) {
	t.Helper()
	if err := ApplyDefaultValues(options); err != nil {
		t.Fatal(err)
	}
	flags, mappings, err := GenerateFlags(options)
	if err != nil {
		t.Fatal(err)
	}
	app := cli.NewApp()
	app.Flags = flags
	app.Action = func(c *cli.Context) error {
		ApplyFlags(flags, mappings, c, options)
		return nil
	}
	if err := app.Run(args); err != nil {
		t.Fatal(err)
	}
}

func TestFlagEnvironmentPriority(t *testing.T) {
	t.Setenv("WEBTMUX_PORT", "9090")

	fromEnvironment := &priorityOptions{}
	runPriorityApp(t, []string{"test"}, fromEnvironment)
	if fromEnvironment.Port != "9090" {
		t.Fatalf("environment port = %q, want 9090", fromEnvironment.Port)
	}

	fromArgument := &priorityOptions{}
	runPriorityApp(t, []string{"test", "--port", "7070"}, fromArgument)
	if fromArgument.Port != "7070" {
		t.Fatalf("argument port = %q, want 7070", fromArgument.Port)
	}
}
