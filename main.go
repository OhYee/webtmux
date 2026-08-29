package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	cli "github.com/urfave/cli/v2"

	"webtmux/backend/localcommand"
	"webtmux/pkg/homedir"
	"webtmux/pkg/logging"
	"webtmux/server"
	"webtmux/utils"
)

func main() {
	environment, err := loadEnvironment(os.Args, os.LookupEnv, os.Setenv)
	if err != nil {
		exit(fmt.Errorf("failed to load environment file: %w", err), 2)
	}
	if err := prepareCredential(os.LookupEnv, os.Setenv); err != nil {
		exit(fmt.Errorf("failed to prepare authentication environment: %w", err), 2)
	}
	authUsername := configuredUsername(os.LookupEnv)
	language := detectLanguage(os.Args, os.Getenv)
	app := cli.NewApp()
	app.Name = "webtmux"
	app.Version = Version
	app.HideHelpCommand = true
	appOptions := &server.Options{}

	if err := utils.ApplyDefaultValues(appOptions); err != nil {
		exit(err, 1)
	}
	backendOptions := &localcommand.Options{}
	if err := utils.ApplyDefaultValues(backendOptions); err != nil {
		exit(err, 1)
	}

	cliFlags, flagMappings, err := utils.GenerateFlags(appOptions, backendOptions)
	if err != nil {
		exit(err, 3)
	}

	app.Flags = append(
		cliFlags,
		&cli.StringFlag{
			Name:    "config",
			Value:   "~/.gotty",
			Usage:   "Config file path",
			EnvVars: []string{"GOTTY_CONFIG"},
		},
		&cli.StringFlag{
			Name:    "lang",
			Value:   language,
			Usage:   "Help language: auto, en, or zh-CN",
			EnvVars: []string{"WEBTMUX_LANG"},
		},
		&cli.StringFlag{
			Name:    "env-file",
			Value:   environment.path,
			Usage:   "Environment file path",
			EnvVars: []string{"WEBTMUX_ENV_FILE"},
		},
	)
	configureHelp(app, language)

	app.Action = func(c *cli.Context) error {
		if c.NArg() == 0 {
			msg := "Error: No command given."
			cli.ShowAppHelp(c)
			exit(fmt.Errorf(msg), 1)
		}

		configFile := c.String("config")
		_, err := os.Stat(homedir.Expand(configFile))
		if configFile != "~/.gotty" || !os.IsNotExist(err) {
			if err := utils.ApplyConfigFile(configFile, appOptions, backendOptions); err != nil {
				exit(err, 2)
			}
		}

		utils.ApplyFlags(cliFlags, flagMappings, c, appOptions, backendOptions)
		if c.IsSet("tls-ca-crt") {
			appOptions.EnableTLSClientAuth = true
		}
		if err := appOptions.Validate(); err != nil {
			exit(err, 6)
		}

		logging.Configure(appOptions.LogLevel, appOptions.LogFormat, appOptions.Quiet)
		if environment.loaded {
			slog.Info("environment file loaded", "path", environment.path)
			if environment.permission&0o077 != 0 {
				slog.Warn("environment file is readable or writable by other users", "path", environment.path, "permission", fmt.Sprintf("%04o", environment.permission))
			}
		}

		// Handle authentication
		if appOptions.NoAuth {
			appOptions.EnableBasicAuth = false
			slog.Warn("authentication disabled; terminal is publicly accessible")
		} else if appOptions.Credential != "" {
			appOptions.EnableBasicAuth = true
		} else {
			// Generate random credentials
			appOptions.EnableBasicAuth = true
			appOptions.Credential = authUsername + ":" + generateRandomPassword(32)
			fmt.Printf("\n")
			fmt.Printf("========================================\n")
			fmt.Printf("  Authentication Required (default)\n")
			fmt.Printf("  Username: %s\n", authUsername)
			fmt.Printf("  Password: %s\n", strings.Split(appOptions.Credential, ":")[1])
			fmt.Printf("========================================\n")
			fmt.Printf("  Use -c user:pass to set custom credentials\n")
			fmt.Printf("  Use --no-auth to disable (not recommended)\n")
			fmt.Printf("========================================\n")
			fmt.Printf("\n")
		}

		args := c.Args()
		factory, err := localcommand.NewFactory(args.First(), args.Tail(), backendOptions)
		if err != nil {
			exit(err, 3)
		}

		hostname, _ := os.Hostname()
		appOptions.TitleVariables = map[string]interface{}{
			"command":  args.First(),
			"argv":     args.Tail(),
			"hostname": hostname,
		}

		srv, err := server.New(factory, appOptions)
		if err != nil {
			exit(err, 3)
		}

		ctx, cancel := context.WithCancel(context.Background())
		gCtx, gCancel := context.WithCancel(context.Background())

		slog.Info("webtmux starting", "version", Version, "command", args.First(), "arg_count", len(args.Tail()),
			"address", appOptions.Address, "port", appOptions.Port, "path", appOptions.Path,
			"auth", appOptions.EnableBasicAuth, "tls", appOptions.EnableTLS, "tmux_session", tmuxSessionForLog(args.Slice()),
			"max_connections", appOptions.MaxConnection, "reader_history", appOptions.ReaderHistory, "max_tmux_rate", appOptions.MaxTmuxRate)
		slog.Debug("child command arguments", "command", args.First(), "args", args.Tail())

		errs := make(chan error, 1)
		go func() {
			errs <- srv.Run(ctx, server.WithGracefullContext(gCtx))
		}()
		err = waitSignals(errs, cancel, gCancel)

		if err != nil {
			// launchd distinguishes an intentionally disabled wrapper (status
			// 0) from a managed process that disappeared (non-zero). When
			// launchctl management is enabled, SIGTERM from a plain `kill`
			// must therefore remain a failure so KeepAlive can restore it.
			// `launchctl bootout` still closes normally because the job itself
			// is unloaded before this status can trigger a restart.
			if err != context.Canceled || appOptions.Launchctl {
				fmt.Printf("Error: %s\n", err)
				exit(err, 8)
			}
		}

		return nil
	}
	app.Run(os.Args)
}

func tmuxSessionForLog(args []string) string {
	for i, arg := range args {
		if (arg == "-s" || arg == "-t") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func exit(err error, code int) {
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(code)
}

func waitSignals(errs chan error, cancel context.CancelFunc, gracefullCancel context.CancelFunc) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(
		sigChan,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	select {
	case err := <-errs:
		return err

	case s := <-sigChan:
		slog.Info("shutdown signal received", "signal", s.String())
		switch s {
		case syscall.SIGINT:
			gracefullCancel()
			slog.Info("graceful shutdown started; press Ctrl-C again to force close")
			select {
			case err := <-errs:
				return err
			case <-sigChan:
				slog.Warn("forcing shutdown after second interrupt")
				cancel()
				return <-errs
			}
		default:
			cancel()
			return <-errs
		}
	}
}

func generateRandomPassword(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)[:length]
}
