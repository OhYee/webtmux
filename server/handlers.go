package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"

	"webtmux/pkg/keys"
	"webtmux/pkg/tmux"
	"webtmux/webtty"
)

var connectionSequence atomic.Uint64

func (server *Server) generateHandleWS(ctx context.Context, cancel context.CancelFunc, counter *counter) http.HandlerFunc {
	once := new(int64)

	go func() {
		select {
		case <-counter.timer().C:
			cancel()
		case <-ctx.Done():
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		connectionID := fmt.Sprintf("c-%d", connectionSequence.Add(1))
		if server.options.Once {
			success := atomic.CompareAndSwapInt64(once, 0, 1)
			if !success {
				http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
				return
			}
		}

		num := counter.add(1)
		closeReason := "unknown reason"

		defer func() {
			num := counter.done()
			log.Printf(
				"Connection closed id=%s reason=%s remote=%s connections=%d/%d",
				connectionID, closeReason, r.RemoteAddr, num, server.options.MaxConnection,
			)

			if server.options.Once {
				cancel()
			}
		}()

		if int64(server.options.MaxConnection) != 0 {
			if num > server.options.MaxConnection {
				closeReason = "exceeding max number of connections"
				http.Error(w, "Too many connections", http.StatusServiceUnavailable)
				return
			}
		}

		slog.Info("client connected", "connection_id", connectionID, "remote", r.RemoteAddr, "connections", num, "max_connections", server.options.MaxConnection, "tmux_session", server.tmuxSession, "tmux_socket", server.tmuxSocket)

		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}

		conn, err := server.upgrader.Upgrade(w, r, nil)
		if err != nil {
			closeReason = err.Error()
			return
		}
		defer conn.Close()
		conn.SetReadLimit(64 * 1024)

		if server.options.PassHeaders {
			err = server.processWSConn(ctx, conn, r.Header, connectionID)
		} else {
			err = server.processWSConn(ctx, conn, nil, connectionID)
		}

		switch err {
		case nil:
			closeReason = "normal completion"
		case ctx.Err():
			closeReason = "cancelation"
		case webtty.ErrSlaveClosed:
			closeReason = server.factory.Name()
		case webtty.ErrMasterClosed:
			closeReason = "client"
		default:
			closeReason = fmt.Sprintf("an error: %s", err)
		}
	}
}

func (server *Server) processWSConn(ctx context.Context, conn *websocket.Conn, headers map[string][]string, connectionID string) error {
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	typ, initLine, err := conn.ReadMessage()
	if err != nil {
		return errors.Wrapf(err, "failed to authenticate websocket connection")
	}
	if typ != websocket.TextMessage {
		return errors.New("failed to authenticate websocket connection: invalid message type")
	}

	var init InitMessage
	err = json.Unmarshal(initLine, &init)
	if err != nil {
		return errors.Wrapf(err, "failed to authenticate websocket connection")
	}
	if init.AuthToken != server.options.Credential {
		return errors.New("failed to authenticate websocket connection")
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	queryPath := "?"
	if server.options.PermitArguments && init.Arguments != "" {
		queryPath = init.Arguments
	}

	query, err := url.Parse(queryPath)
	if err != nil {
		return errors.Wrapf(err, "failed to parse arguments")
	}
	params := query.Query()
	var slave Slave
	slave, err = server.factory.New(params, headers)
	if err != nil {
		return errors.Wrapf(err, "failed to create backend")
	}
	defer slave.Close()

	titleVars := server.titleVariables(
		[]string{"server", "master", "slave"},
		map[string]map[string]interface{}{
			"server": server.options.TitleVariables,
			"master": map[string]interface{}{
				"remote_addr": conn.RemoteAddr(),
			},
			"slave": slave.WindowTitleVariables(),
		},
	)

	titleBuf := new(bytes.Buffer)
	err = server.titleTemplate.Execute(titleBuf, titleVars)
	if err != nil {
		return errors.Wrapf(err, "failed to fill window title template")
	}

	opts := []webtty.Option{
		webtty.WithWindowTitle(titleBuf.Bytes()),
	}
	if server.tmuxSession != "" {
		opts = append(opts, webtty.WithResizePermit(server.tmuxGuard.Record))
	}
	if server.options.PermitWrite {
		opts = append(opts, webtty.WithPermitWrite())
	}
	if server.options.EnableReconnect {
		opts = append(opts, webtty.WithReconnect(server.options.ReconnectTime))
	}
	if server.options.Width > 0 {
		opts = append(opts, webtty.WithFixedColumns(server.options.Width))
	}
	if server.options.Height > 0 {
		opts = append(opts, webtty.WithFixedRows(server.options.Height))
	}
	tty, err := webtty.New(&wsWrapper{conn}, slave, opts...)
	if err != nil {
		return errors.Wrapf(err, "failed to create webtty")
	}

	// The PTY this connection started is one of "everything we spawned", so it
	// goes down with the guard too.
	unregisterSlave := server.tmuxGuard.OnTrip(func() { slave.Close() })
	defer unregisterSlave()

	// Give this connection its own tmux controller.
	//
	// The controller carries mutable per-viewer state — which session is being
	// looked at, and the activity history behind the watch board. Sharing one
	// across connections means a phone switching sessions silently redirects
	// whatever the desktop is showing.
	if server.tmuxSession != "" {
		ctrl, ctrlErr := tmux.NewController(server.tmuxSession, server.tmuxSocket, server.tmuxWatch)
		if ctrlErr == nil {
			// Share one control connection and one ceiling across every viewer.
			ctrl.UseGuard(server.tmuxGuard)
			ctrl.UseRunner(server.tmuxExec)
		}
		if ctrlErr != nil {
			slog.Warn("failed to create tmux controller", "connection_id", connectionID, "session", server.tmuxSession, "socket", server.tmuxSocket, "error", ctrlErr)
		} else if startErr := ctrl.Start(); startErr != nil {
			// The controller already has a worker goroutine running, so a
			// failed start still has to be torn down or every reconnect
			// against an unreachable tmux leaks one.
			ctrl.Stop()
			slog.Warn("failed to start tmux controller", "connection_id", connectionID, "session", server.tmuxSession, "socket", server.tmuxSocket, "error", startErr)
		} else {
			// Pin session switching to the client this connection spawned.
			if p, ok := slave.(interface{ Pid() int }); ok {
				ctrl.SetClientPID(p.Pid())
			}
			defer ctrl.Stop()
			tty.SetTmuxController(ctrl)

			// Scope the poller to this connection. Handing it the server's
			// context leaks a goroutine per connection that keeps shelling out
			// to tmux every 500ms forever — after a day of a phone
			// reconnecting, that is thousands of tmux invocations a second
			// against a server nobody is watching.
			pollCtx, stopPolling := context.WithCancel(ctx)
			defer stopPolling()
			go server.handleTmuxEvents(pollCtx, tty, ctrl)
		}
	}

	err = tty.Run(ctx)

	return err
}

// handleTmuxEvents polls for tmux layout changes and sends updates to the client
func (server *Server) handleTmuxEvents(ctx context.Context, tty *webtty.WebTTY, ctrl *tmux.Controller) {
	if ctrl == nil {
		return
	}

	// tmux says when the layout moved, so this waits rather than asks. The
	// slow ticker is only a backstop for a notification we failed to classify,
	// and for the case where control mode is unavailable and nothing will ever
	// wake us.
	moved, unsubscribe := server.layoutDirty.subscribe()
	defer unsubscribe()

	var last uint64
	var lastRefreshError time.Time
	refreshFailing := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-moved:
		}

		if err := ctrl.RefreshLayout(); err != nil {
			if !refreshFailing || time.Since(lastRefreshError) >= 30*time.Second {
				slog.Warn("tmux layout refresh failed", "session", server.tmuxSession, "socket", server.tmuxSocket, "error", err)
				lastRefreshError = time.Now()
			}
			refreshFailing = true
			continue
		}
		if refreshFailing {
			slog.Info("tmux layout refresh recovered", "session", server.tmuxSession, "socket", server.tmuxSocket)
			refreshFailing = false
		}
		layout := ctrl.GetLayout()
		if layout == nil {
			continue
		}

		// Compare a hash rather than keeping the whole JSON around; the layout
		// is marshalled again inside SendTmuxLayout anyway.
		data, err := json.Marshal(layout)
		if err != nil {
			continue
		}
		h := fnv.New64a()
		h.Write(data)
		if sum := h.Sum64(); sum != last {
			last = sum
			if err := tty.SendTmuxLayout(); err != nil {
				slog.Warn("failed to send tmux layout", "session", server.tmuxSession, "socket", server.tmuxSocket, "error", err)
			}
		}
	}
}

func (server *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	indexVars, err := server.indexVariables(r)
	if err != nil {
		slog.Error("failed to render index variables", "remote", r.RemoteAddr, "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	indexBuf := new(bytes.Buffer)
	err = server.indexTemplate.Execute(indexBuf, indexVars)
	if err != nil {
		slog.Error("failed to render index template", "remote", r.RemoteAddr, "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	w.Write(indexBuf.Bytes())
}

func (server *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	indexVars, err := server.indexVariables(r)
	if err != nil {
		slog.Error("failed to render manifest variables", "remote", r.RemoteAddr, "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	indexBuf := new(bytes.Buffer)
	err = server.manifestTemplate.Execute(indexBuf, indexVars)
	if err != nil {
		slog.Error("failed to render manifest template", "remote", r.RemoteAddr, "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	w.Write(indexBuf.Bytes())
}

func (server *Server) indexVariables(r *http.Request) (map[string]interface{}, error) {
	titleVars := server.titleVariables(
		[]string{"server", "master"},
		map[string]map[string]interface{}{
			"server": server.options.TitleVariables,
			"master": map[string]interface{}{
				"remote_addr": r.RemoteAddr,
			},
		},
	)

	titleBuf := new(bytes.Buffer)
	err := server.titleTemplate.Execute(titleBuf, titleVars)
	if err != nil {
		return nil, err
	}

	indexVars := map[string]interface{}{
		"title": titleBuf.String(),
	}
	return indexVars, err
}

func (server *Server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	credential, err := json.Marshal(server.options.Credential)
	if err != nil {
		slog.Error("failed to encode authentication token", "remote", r.RemoteAddr, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(append(append([]byte("var gotty_auth_token = "), credential...), ';'))
}

func (server *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	lines := []string{
		"var gotty_term = 'xterm';",
		"var gotty_ws_query_args = '" + server.options.WSQueryArgs + "';",
	}

	w.Write([]byte(strings.Join(lines, "\n")))
}

// handleKeys serves the on-screen key panel definition. A malformed custom
// file degrades to the built-in panel rather than leaving touch clients with no
// way to reach modified keys at all.
func (server *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	panel, err := keys.Load(server.options.KeysFile)
	if err != nil {
		slog.Warn("failed to load key panel; using built-in panel", "file", server.options.KeysFile, "error", err)
		panel = keys.Default()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(panel); err != nil {
		slog.Warn("failed to write key panel response", "remote", r.RemoteAddr, "error", err)
	}
}

// handleKeysConfig edits only the file explicitly named by --keys. With no
// configured file the current defaults remain inspectable, but the browser
// cannot choose an arbitrary server-side path to write.
func (server *Server) handleKeysConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		panel, err := keys.Load(server.options.KeysFile)
		var warning string
		if err != nil {
			warning = err.Error()
			if data, readErr := os.ReadFile(server.options.KeysFile); readErr == nil {
				json.NewEncoder(w).Encode(map[string]any{
					"writable": true,
					"valid":    false,
					"content":  string(data),
					"error":    warning,
				})
				return
			}
			panel = keys.Default()
		}

		data, _ := json.MarshalIndent(panel, "", "  ")
		json.NewEncoder(w).Encode(map[string]any{
			"writable": server.options.KeysFile != "",
			"valid":    true,
			"content":  string(data),
			"error":    warning,
		})

	case http.MethodPut:
		if server.options.KeysFile == "" {
			http.Error(w, `{"error":"start webtmux with --keys FILE to enable saving"}`, http.StatusConflict)
			return
		}

		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		var panel keys.Panel
		if err := decoder.Decode(&panel); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, `{"error":"key configuration must contain one JSON object"}`, http.StatusBadRequest)
			return
		}
		if err := keys.Save(server.options.KeysFile, &panel); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}

		data, _ := json.MarshalIndent(&panel, "", "  ")
		json.NewEncoder(w).Encode(map[string]any{
			"writable": true,
			"valid":    true,
			"content":  string(data),
		})

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// titleVariables merges maps in a specified order.
// varUnits are name-keyed maps, whose names will be iterated using order.
func (server *Server) titleVariables(order []string, varUnits map[string]map[string]interface{}) map[string]interface{} {
	titleVars := map[string]interface{}{}

	for _, name := range order {
		vars, ok := varUnits[name]
		if !ok {
			panic("title variable name error")
		}
		for key, val := range vars {
			titleVars[key] = val
		}
	}

	// safe net for conflicted keys
	for _, name := range order {
		titleVars[name] = varUnits[name]
	}

	return titleVars
}
