package server

import (
	"github.com/pkg/errors"
)

type Options struct {
	Address             string `hcl:"address" flagName:"address" flagSName:"a" flagDescribe:"IP address to listen" default:"0.0.0.0"`
	Port                string `hcl:"port" flagName:"port" flagSName:"p" flagDescribe:"Port number to liten" default:"8080"`
	Path                string `hcl:"path" flagName:"path" flagSName:"m" flagDescribe:"Base path" default:"/"`
	PermitWrite         bool   `hcl:"permit_write" flagName:"permit-write" flagSName:"w" flagDescribe:"Permit clients to write to the TTY (BE CAREFUL)" default:"false"`
	EnableBasicAuth     bool   `hcl:"enable_basic_auth" default:"true"`
	Credential          string `hcl:"credential" flagName:"credential" flagSName:"c" flagDescribe:"Credential for Basic Authentication (ex: user:pass)" default:""`
	NoAuth              bool   `hcl:"no_auth" flagName:"no-auth" flagDescribe:"Disable authentication (NOT RECOMMENDED)" default:"false"`
	EnableRandomUrl     bool   `hcl:"enable_random_url" flagName:"random-url" flagSName:"r" flagDescribe:"Add a random string to the URL" default:"false"`
	RandomUrlLength     int    `hcl:"random_url_length" flagName:"random-url-length" flagDescribe:"Random URL length" default:"8"`
	EnableTLS           bool   `hcl:"enable_tls" flagName:"tls" flagSName:"t" flagDescribe:"Enable TLS/SSL" default:"false"`
	TLSCrtFile          string `hcl:"tls_crt_file" flagName:"tls-crt" flagDescribe:"TLS/SSL certificate file path" default:"~/.gotty.crt"`
	TLSKeyFile          string `hcl:"tls_key_file" flagName:"tls-key" flagDescribe:"TLS/SSL key file path" default:"~/.gotty.key"`
	EnableTLSClientAuth bool   `hcl:"enable_tls_client_auth" default:"false"`
	TLSCACrtFile        string `hcl:"tls_ca_crt_file" flagName:"tls-ca-crt" flagDescribe:"TLS/SSL CA certificate file for client certifications" default:"~/.gotty.ca.crt"`
	IndexFile           string `hcl:"index_file" flagName:"index" flagDescribe:"Custom index.html file" default:""`
	TitleFormat         string `hcl:"title_format" flagName:"title-format" flagSName:"" flagDescribe:"Title format of browser window" default:"{{ .command }}@{{ .hostname }}"`
	EnableReconnect     bool   `hcl:"enable_reconnect" flagName:"reconnect" flagDescribe:"Enable reconnection" default:"false"`
	ReconnectTime       int    `hcl:"reconnect_time" flagName:"reconnect-time" flagDescribe:"Time to reconnect" default:"10"`
	MaxConnection       int    `hcl:"max_connection" flagName:"max-connection" flagDescribe:"Maximum connection to gotty (0 disables the limit)" default:"32"`
	Once                bool   `hcl:"once" flagName:"once" flagDescribe:"Accept only one client and exit on disconnection" default:"false"`
	Timeout             int    `hcl:"timeout" flagName:"timeout" flagDescribe:"Timeout seconds for waiting a client(0 to disable)" default:"0"`
	PermitArguments     bool   `hcl:"permit_arguments" flagName:"permit-arguments" flagDescribe:"Permit clients to send command line arguments in URL (e.g. http://example.com:8080/?arg=AAA&arg=BBB)" default:"false"`
	PassHeaders         bool   `hcl:"pass_headers" flagName:"pass-headers" flagDescribe:"Pass HTTP request headers as environment variables (e.g. Cookie becomes HTTP_COOKIE)" default:"false"`
	Width               int    `hcl:"width" flagName:"width" flagDescribe:"Static width of the screen, 0(default) means dynamically resize" default:"0"`
	Height              int    `hcl:"height" flagName:"height" flagDescribe:"Static height of the screen, 0(default) means dynamically resize" default:"0"`
	WSOrigin            string `hcl:"ws_origin" flagName:"ws-origin" flagDescribe:"A regular expression that matches origin URLs to be accepted by WebSocket. No cross origin requests are acceptable by default" default:""`
	WSQueryArgs         string `hcl:"ws_query_args" flagName:"ws-query-args" flagDescribe:"Querystring arguments to append to the websocket instantiation" default:""`
	EnableWebGL         bool   `hcl:"enable_webgl" flagName:"enable-webgl" flagDescribe:"Enable WebGL renderer" default:"true"`
	KeysFile            string `hcl:"keys_file" flagName:"keys" flagDescribe:"Custom on-screen key panel definition (JSON); uses the built-in panel when unset" default:""`
	TmuxSocket          string `hcl:"tmux_socket" flagName:"tmux-socket" flagDescribe:"tmux server to control: a socket name (like -L) or a path containing a slash (like -S); inferred from the tmux command line when unset" default:""`
	MaxTmuxRate         int    `hcl:"max_tmux_rate" flagName:"max-tmux-rate" flagDescribe:"Halt all tmux access if this server exceeds this many tmux commands in one second or on average over 10s, and shut down what it started. 0 disables the check" default:"40"`
	ReaderHistory       int    `hcl:"reader_history" flagName:"reader-history" flagDescribe:"How many lines of scrollback the reader may load for one pane. Higher costs transfer: roughly 90KB per 1000 lines, re-sent each time you scroll further back" default:"5000"`
	Launchctl           bool   `hcl:"launchctl" default:"false"`
	Quiet               bool   `hcl:"quiet" flagName:"quiet" flagDescribe:"Don't log" default:"false"`

	TitleVariables map[string]interface{}
}

func (options *Options) Validate() error {
	if options.EnableTLSClientAuth && !options.EnableTLS {
		return errors.New("TLS client authentication is enabled, but TLS is not enabled")
	}
	return nil
}
