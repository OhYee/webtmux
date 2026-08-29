package main

import (
	"testing"

	cli "github.com/urfave/cli/v2"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{name: "defaults to English", args: []string{"webtmux"}, want: "en"},
		{name: "Chinese locale", args: []string{"webtmux"}, env: map[string]string{"LANG": "zh_CN.UTF-8"}, want: "zh-CN"},
		{name: "environment override", args: []string{"webtmux"}, env: map[string]string{"WEBTMUX_LANG": "zh-CN", "LANG": "en_US.UTF-8"}, want: "zh-CN"},
		{name: "argument override", args: []string{"webtmux", "--lang=en"}, env: map[string]string{"WEBTMUX_LANG": "zh-CN"}, want: "en"},
		{name: "separate argument", args: []string{"webtmux", "--lang", "zh-CN"}, want: "zh-CN"},
		{name: "unsupported language falls back", args: []string{"webtmux", "--lang", "fr"}, want: "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			if got := detectLanguage(tt.args, getenv); got != tt.want {
				t.Fatalf("detectLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigureHelpLocalizesFlags(t *testing.T) {
	app := cli.NewApp()
	port := &cli.StringFlag{Name: "port", Usage: "old"}
	app.Flags = []cli.Flag{port}

	configureHelp(app, "zh-CN")

	if port.Usage != "监听端口" {
		t.Fatalf("port usage = %q", port.Usage)
	}
	if app.UsageText != "webtmux [选项] <命令> [参数...]" {
		t.Fatalf("usage text = %q", app.UsageText)
	}
}
