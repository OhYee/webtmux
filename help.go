package main

import (
	"strings"

	cli "github.com/urfave/cli/v2"
)

type helpText struct {
	usage, usageText, description           string
	name, synopsis, version, options        string
	examples, environment, config, security string
	helpUsage, versionUsage                 string
	flagUsage                               map[string]string
}

var helpTexts = map[string]helpText{
	"en": {
		usage:       "Web terminal for tmux with a visual pane layout",
		usageText:   "webtmux [options] <command> [arguments...]",
		description: "Run a local command in a browser-based terminal. webtmux adds tmux-aware pane navigation, touch controls, and scrollback support.\n\nThe command and every argument after it are passed to the child process. For an interactive terminal, enable input with --permit-write.",
		name:        "NAME", synopsis: "SYNOPSIS", version: "VERSION", options: "OPTIONS",
		examples:    "EXAMPLES:\n   webtmux -w tmux new-session -A -s main\n   webtmux -w -c user:password -p 9090 tmux attach -t main\n   webtmux --tls --tls-crt server.crt --tls-key server.key -w tmux",
		environment: "ENVIRONMENT:\n   WEBTMUX_PORT             Listening port.\n   WEBTMUX_USERNAME         Basic Auth username (default: admin).\n   WEBTMUX_PASSWORD         Basic Auth password.\n   WEBTMUX_<OPTION>         Sets any option; legacy GOTTY_<OPTION> also works.\n   Existing environment values override .env; CLI options override both.",
		config:      "CONFIGURATION:\n   .env is loaded from the working directory when present; select another file\n   with --env-file FILE. The legacy ~/.gotty HCL file remains supported through\n   --config FILE. Keep files containing passwords readable only by their owner.",
		security:    "SECURITY:\n   Authentication is enabled by default. If --credential is omitted, a random\n   admin password is printed at startup. Avoid --no-auth on untrusted networks.",
		helpUsage:   "Show help", versionUsage: "Print the version",
		flagUsage: englishFlagUsage,
	},
	"zh-CN": {
		usage:       "具有可视化窗格布局的 tmux Web 终端",
		usageText:   "webtmux [选项] <命令> [参数...]",
		description: "在基于浏览器的终端中运行本地命令。webtmux 提供 tmux 窗格导航、触摸控制和回滚历史支持。\n\n命令及其后的全部参数会原样传递给子进程。交互式使用时请通过 --permit-write 允许输入。",
		name:        "名称", synopsis: "用法", version: "版本", options: "选项",
		examples:    "示例：\n   webtmux -w tmux new-session -A -s main\n   webtmux -w -c user:password -p 9090 tmux attach -t main\n   webtmux --tls --tls-crt server.crt --tls-key server.key -w tmux",
		environment: "环境变量：\n   WEBTMUX_PORT             监听端口。\n   WEBTMUX_USERNAME         Basic Auth 用户名（默认 admin）。\n   WEBTMUX_PASSWORD         Basic Auth 密码。\n   WEBTMUX_<OPTION>         设置任意选项；同时兼容 GOTTY_<OPTION>。\n   已有环境变量覆盖 .env，命令行参数覆盖两者。",
		config:      "配置：\n   默认读取工作目录中的 .env，也可用 --env-file FILE 指定其他文件。\n   仍可通过 --config FILE 使用旧版 ~/.gotty HCL 配置文件。包含密码的\n   文件应仅允许文件所有者读取。",
		security:    "安全：\n   默认启用身份验证。未指定 --credential 时，启动时会输出随机的 admin\n   密码。请勿在不可信网络中使用 --no-auth。",
		helpUsage:   "显示帮助", versionUsage: "显示版本",
		flagUsage: chineseFlagUsage,
	},
}

var englishFlagUsage = map[string]string{
	"address": "IP address to listen on", "port": "Port to listen on", "path": "Base URL path",
	"permit-write": "Allow clients to send input to the terminal (use with care)", "credential": "HTTP Basic Auth credentials (user:password)",
	"no-auth": "Disable authentication (not recommended)", "random-url": "Add a random component to the URL", "random-url-length": "Length of the random URL component",
	"tls": "Enable TLS", "tls-crt": "TLS certificate file", "tls-key": "TLS private key file", "tls-ca-crt": "CA certificate used to authenticate clients",
	"index": "Custom index.html file", "title-format": "Browser window title template", "reconnect": "Reconnect automatically after disconnection",
	"reconnect-time": "Seconds between reconnection attempts", "max-connection": "Maximum concurrent connections (0 means unlimited)",
	"once": "Accept one client and exit after it disconnects", "timeout": "Seconds to wait for a client (0 disables the timeout)",
	"permit-arguments": "Allow clients to append command arguments through URL arg parameters", "pass-headers": "Expose HTTP request headers to the command as HTTP_* environment variables",
	"width": "Fixed terminal width (0 resizes dynamically)", "height": "Fixed terminal height (0 resizes dynamically)",
	"ws-origin": "Regular expression for allowed WebSocket Origin URLs", "ws-query-args": "Query string appended when opening the WebSocket",
	"enable-webgl": "Use the WebGL terminal renderer", "keys": "JSON file defining the on-screen key panel",
	"tmux-socket":    "tmux socket name (-L style) or socket path (-S style); inferred when omitted",
	"max-tmux-rate":  "Maximum tmux commands per second (also averaged over 10s); 0 disables the limit",
	"reader-history": "Maximum scrollback lines loaded per pane (about 90 KB per 1000 lines)", "quiet": "Disable logging",
	"close-signal": "Signal sent when closing the child command (default: SIGHUP)", "close-timeout": "Seconds before force-killing a disconnected child (-1 disables)",
	"config": "HCL configuration file", "lang": "Help language: auto, en, or zh-CN",
	"log-level": "Log level: error, warn, info, or debug", "log-format": "Log format: text or json",
	"env-file": "Environment file path",
}

var chineseFlagUsage = map[string]string{
	"address": "监听 IP 地址", "port": "监听端口", "path": "URL 基础路径", "permit-write": "允许客户端向终端输入（请谨慎使用）",
	"credential": "HTTP Basic Auth 凭据（用户:密码）", "no-auth": "禁用身份验证（不推荐）", "random-url": "在 URL 中加入随机路径", "random-url-length": "随机路径长度",
	"tls": "启用 TLS", "tls-crt": "TLS 证书文件", "tls-key": "TLS 私钥文件", "tls-ca-crt": "用于验证客户端的 CA 证书",
	"index": "自定义 index.html 文件", "title-format": "浏览器窗口标题模板", "reconnect": "断开后自动重连", "reconnect-time": "重连间隔秒数",
	"max-connection": "最大并发连接数（0 表示无限制）", "once": "仅接受一个客户端，并在其断开后退出", "timeout": "等待客户端的秒数（0 表示不超时）",
	"permit-arguments": "允许客户端通过 URL arg 参数追加命令参数", "pass-headers": "以 HTTP_* 环境变量向命令传递 HTTP 请求头",
	"width": "固定终端宽度（0 表示动态调整）", "height": "固定终端高度（0 表示动态调整）", "ws-origin": "允许的 WebSocket Origin URL 正则表达式",
	"ws-query-args": "建立 WebSocket 时附加的查询字符串", "enable-webgl": "使用 WebGL 终端渲染器", "keys": "定义屏幕按键面板的 JSON 文件",
	"tmux-socket": "tmux socket 名称（-L）或路径（-S）；未设置时自动推断", "max-tmux-rate": "每秒最大 tmux 命令数（也按 10 秒平均）；0 表示禁用限制",
	"reader-history": "每个窗格最多加载的回滚行数（每 1000 行约 90 KB）", "quiet": "禁用日志",
	"close-signal": "关闭子命令时发送的信号（默认 SIGHUP）", "close-timeout": "断开后强制终止子进程前等待的秒数（-1 表示禁用）",
	"config": "HCL 配置文件", "lang": "帮助语言：auto、en 或 zh-CN",
	"log-level": "日志级别：error、warn、info 或 debug", "log-format": "日志格式：text 或 json",
	"env-file": "环境变量文件路径",
}

func detectLanguage(args []string, getenv func(string) string) string {
	requested := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--lang" && i+1 < len(args) {
			requested = args[i+1]
			break
		}
		if strings.HasPrefix(args[i], "--lang=") {
			requested = strings.TrimPrefix(args[i], "--lang=")
			break
		}
	}
	if requested == "" {
		requested = getenv("WEBTMUX_LANG")
	}
	if requested == "" || strings.EqualFold(requested, "auto") {
		for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if value := getenv(key); value != "" {
				requested = value
				break
			}
		}
	}
	requested = strings.ToLower(strings.ReplaceAll(requested, "_", "-"))
	if strings.HasPrefix(requested, "zh") {
		return "zh-CN"
	}
	return "en"
}

func configureHelp(app *cli.App, language string) {
	t, ok := helpTexts[language]
	if !ok {
		t = helpTexts["en"]
	}
	app.Usage, app.UsageText, app.Description = t.usage, t.usageText, t.description
	for _, flag := range app.Flags {
		if usage, found := t.flagUsage[flag.Names()[0]]; found {
			setFlagUsage(flag, usage)
		}
	}
	cli.HelpFlag = &cli.BoolFlag{Name: "help", Aliases: []string{"h"}, Usage: t.helpUsage}
	cli.VersionFlag = &cli.BoolFlag{Name: "version", Aliases: []string{"v"}, Usage: t.versionUsage}
	app.CustomAppHelpTemplate = t.name + `:
   {{.Name}}{{if .Usage}} - {{.Usage}}{{end}}

` + t.synopsis + `:
   {{.UsageText}}

` + t.version + `:
   {{.Version}}

{{if .Description}}{{.Description | nindent 3 | trim}}

{{end}}` + t.options + `:
   {{range $index, $option := .VisibleFlags}}{{if $index}}
   {{end}}{{$option}}{{end}}

` + t.examples + "\n\n" + t.environment + "\n\n" + t.config + "\n\n" + t.security + "\n"
}

func setFlagUsage(flag cli.Flag, usage string) {
	switch f := flag.(type) {
	case *cli.StringFlag:
		f.Usage = usage
	case *cli.BoolFlag:
		f.Usage = usage
	case *cli.IntFlag:
		f.Usage = usage
	}
}
