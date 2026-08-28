package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	noesctmpl "text/template"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"

	"webtmux/bindata"
	"webtmux/pkg/homedir"
	"webtmux/pkg/randomstring"
	"webtmux/pkg/tmux"
	"webtmux/webtty"
)

// Server provides a webtty HTTP endpoint.
type Server struct {
	factory Factory
	options *Options

	upgrader         *websocket.Upgrader
	indexTemplate    *template.Template
	titleTemplate    *noesctmpl.Template
	manifestTemplate *template.Template

	// Tmux support
	tmuxSession string
	tmuxSocket  string
	// One capture cache for the whole server, shared across connections.
	tmuxWatch *tmux.WatchTracker

	// One bounded, recoverable command path for every browser.
	tmuxExec *tmux.Executor

	// layoutDirty wakes viewers when tmux says the layout moved.
	layoutDirty *dirty

	// tmuxGuard halts everything if this server starts overloading tmux.
	tmuxGuard *tmux.Guard
}

// New creates a new instance of Server.
// Server will use the New() of the factory provided to handle each request.
func New(factory Factory, options *Options) (*Server, error) {
	indexData, err := bindata.Fs.ReadFile("static/index.html")
	if err != nil {
		panic("index not found") // must be in bindata
	}
	if options.IndexFile != "" {
		path := homedir.Expand(options.IndexFile)
		indexData, err = os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read custom index file at `%s`", path)
		}
	}
	indexTemplate, err := template.New("index").Parse(string(indexData))
	if err != nil {
		panic("index template parse failed") // must be valid
	}

	manifestData, err := bindata.Fs.ReadFile("static/manifest.json")
	if err != nil {
		panic("manifest not found") // must be in bindata
	}
	manifestTemplate, err := template.New("manifest").Parse(string(manifestData))
	if err != nil {
		panic("manifest template parse failed") // must be valid
	}

	titleTemplate, err := noesctmpl.New("title").Parse(options.TitleFormat)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse window title format `%s`", options.TitleFormat)
	}

	var originChekcer func(r *http.Request) bool
	if options.WSOrigin != "" {
		matcher, err := regexp.Compile(options.WSOrigin)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to compile regular expression of Websocket Origin: %s", options.WSOrigin)
		}
		originChekcer = func(r *http.Request) bool {
			return matcher.MatchString(r.Header.Get("Origin"))
		}
	} else {
		// Default: allow all origins (auth provides protection)
		originChekcer = func(r *http.Request) bool {
			return true
		}
	}

	server := &Server{
		factory: factory,
		options: options,

		upgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Subprotocols:    webtty.Protocols,
			CheckOrigin:     originChekcer,
		},
		indexTemplate:    indexTemplate,
		titleTemplate:    titleTemplate,
		manifestTemplate: manifestTemplate,
	}

	server.tmuxWatch = tmux.NewWatchTracker()
	server.layoutDirty = newDirty()
	server.tmuxGuard = tmux.NewGuard(options.MaxTmuxRate)
	tmux.SetMaxCaptureLines(options.ReaderHistory)

	// Detect tmux session from command
	server.tmuxSession, server.tmuxSocket = server.detectTmux()
	if server.tmuxSession != "" {
		server.tmuxExec = tmux.NewExecutor(server.tmuxSocket, server.tmuxSession, server.tmuxGuard, func(ctl *tmux.Control) {
			go server.pumpEvents(ctl)
		})
		server.tmuxGuard.OnTrip(server.tmuxExec.Close)
	}

	if server.tmuxSession != "" {
		if server.tmuxSocket != "" {
			log.Printf("Detected tmux session: %s (socket %s)", server.tmuxSession, server.tmuxSocket)
		} else {
			log.Printf("Detected tmux session: %s", server.tmuxSession)
		}
	}

	return server, nil
}

// detectTmux checks if we're running tmux and extracts the session name and
// the server socket. Reading the socket off the same command line means
// `webtmux -w tmux -L work attach -t main` drives the -L work server rather
// than silently falling back to the default one.
func (server *Server) detectTmux() (session, socket string) {
	cmd, argv := server.factory.Command()

	// Check if command is tmux
	if !strings.HasSuffix(cmd, "tmux") && cmd != "tmux" {
		return "", ""
	}

	// Parse argv to find session name and socket.
	// Common patterns:
	// tmux new-session -A -s <name>
	// tmux attach -t <name>
	// tmux -L <socket> attach-session -t <name>
	for i, arg := range argv {
		if i+1 >= len(argv) {
			break
		}
		switch arg {
		case "-s", "-t":
			if session == "" {
				session = argv[i+1]
			}
		case "-L", "-S":
			socket = argv[i+1]
		}
	}

	// An explicit flag wins over whatever the command line implies.
	if server.options.TmuxSocket != "" {
		socket = server.options.TmuxSocket
	}

	if session == "" {
		// Default session name if not specified
		session = "0"
	}

	return session, socket
}

// Run starts the main process of the Server.
// The cancelation of ctx will shutdown the server immediately with aborting
// existing connections. Use WithGracefullContext() to support gracefull shutdown.
func (server *Server) Run(ctx context.Context, options ...RunOption) error {
	cctx, cancel := context.WithCancel(ctx)
	opts := &RunOptions{gracefullCtx: context.Background()}
	for _, opt := range options {
		opt(opts)
	}

	// Controllers are created per connection, not here: see processWSConn.

	counter := newCounter(time.Duration(server.options.Timeout) * time.Second)

	path := server.options.Path
	if server.options.EnableRandomUrl {
		path = "/" + randomstring.Generate(server.options.RandomUrlLength) + "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	handlers := server.setupHandlers(cctx, cancel, path, counter)
	srv, err := server.setupHTTPServer(handlers)
	if err != nil {
		return errors.Wrapf(err, "failed to setup an HTTP server")
	}

	if server.options.PermitWrite {
		log.Printf("Permitting clients to write input to the PTY.")
	}
	if server.options.Once {
		log.Printf("Once option is provided, accepting only one client")
	}

	if server.options.Port == "0" {
		log.Printf("Port number configured to `0`, choosing a random port")
	}
	hostPort := net.JoinHostPort(server.options.Address, server.options.Port)
	listener, err := net.Listen("tcp", hostPort)
	if err != nil {
		return errors.Wrapf(err, "failed to listen at `%s`", hostPort)
	}

	scheme := "http"
	if server.options.EnableTLS {
		scheme = "https"
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	log.Printf("HTTP server is listening at: %s", scheme+"://"+net.JoinHostPort(host, port)+path)
	if server.options.Address == "0.0.0.0" {
		for _, address := range listAddresses() {
			log.Printf("Alternative URL: %s", scheme+"://"+net.JoinHostPort(address, port)+path)
		}
	}

	srvErr := make(chan error, 1)
	go func() {
		if server.options.EnableTLS {
			crtFile := homedir.Expand(server.options.TLSCrtFile)
			keyFile := homedir.Expand(server.options.TLSKeyFile)
			log.Printf("TLS crt file: " + crtFile)
			log.Printf("TLS key file: " + keyFile)

			err = srv.ServeTLS(listener, crtFile, keyFile)
		} else {
			err = srv.Serve(listener)
		}
		if err != nil {
			srvErr <- err
		}
	}()

	go func() {
		select {
		case <-opts.gracefullCtx.Done():
			srv.Shutdown(context.Background())
		case <-cctx.Done():
		}
	}()

	select {
	case err = <-srvErr:
		if err == http.ErrServerClosed { // by gracefull ctx
			err = nil
		} else {
			cancel()
		}
	case <-cctx.Done():
		srv.Close()
		err = cctx.Err()
	}

	conn := counter.count()
	if conn > 0 {
		log.Printf("Waiting for %d connections to be closed", conn)
	}
	counter.wait()

	return err
}

func (server *Server) setupHandlers(ctx context.Context, cancel context.CancelFunc, pathPrefix string, counter *counter) http.Handler {
	fs, err := fs.Sub(bindata.Fs, "static")
	if err != nil {
		log.Fatalf("failed to open static/ subdirectory of embedded filesystem: %v", err)
	}
	staticFileHandler := http.FileServer(http.FS(fs))

	var siteMux = http.NewServeMux()
	siteMux.HandleFunc(pathPrefix, server.handleIndex)
	siteMux.Handle(pathPrefix+"js/", http.StripPrefix(pathPrefix, staticFileHandler))
	siteMux.Handle(pathPrefix+"favicon.ico", http.StripPrefix(pathPrefix, staticFileHandler))
	siteMux.Handle(pathPrefix+"icon.svg", http.StripPrefix(pathPrefix, staticFileHandler))
	siteMux.Handle(pathPrefix+"css/", http.StripPrefix(pathPrefix, staticFileHandler))
	siteMux.Handle(pathPrefix+"icon_192.png", http.StripPrefix(pathPrefix, staticFileHandler))

	siteMux.HandleFunc(pathPrefix+"manifest.json", server.handleManifest)
	siteMux.HandleFunc(pathPrefix+"auth_token.js", server.handleAuthToken)
	siteMux.HandleFunc(pathPrefix+"config.js", server.handleConfig)
	siteMux.HandleFunc(pathPrefix+"keys.json", server.handleKeys)
	siteMux.HandleFunc(pathPrefix+"keys-config.json", server.handleKeysConfig)

	siteHandler := http.Handler(siteMux)

	if server.options.EnableBasicAuth {
		log.Printf("Using Basic Authentication")
		siteHandler = server.wrapBasicAuth(siteHandler, server.options.Credential)
	}

	withGz := gziphandler.GzipHandler(server.wrapHeaders(siteHandler))
	siteHandler = server.wrapLogger(withGz)

	wsMux := http.NewServeMux()
	wsMux.Handle("/", siteHandler)
	wsHandler := http.Handler(http.HandlerFunc(server.generateHandleWS(ctx, cancel, counter)))
	if server.options.EnableBasicAuth {
		wsHandler = server.wrapBasicAuth(wsHandler, server.options.Credential)
	}
	wsMux.Handle(pathPrefix+"ws", wsHandler)
	siteHandler = http.Handler(wsMux)

	return siteHandler
}

func (server *Server) setupHTTPServer(handler http.Handler) (*http.Server, error) {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	if server.options.EnableTLSClientAuth {
		tlsConfig, err := server.tlsConfig()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to setup TLS configuration")
		}
		srv.TLSConfig = tlsConfig
	}

	return srv, nil
}

func (server *Server) tlsConfig() (*tls.Config, error) {
	caFile := homedir.Expand(server.options.TLSCACrtFile)
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("could not open CA crt file " + caFile)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("could not parse CA crt file data in " + caFile)
	}
	tlsConfig := &tls.Config{
		ClientCAs:  caCertPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
	return tlsConfig, nil
}
