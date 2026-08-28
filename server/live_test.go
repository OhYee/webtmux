package server

import (
	"encoding/base64"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"webtmux/webtty"
)

// TestLiveWebSocketLayout is opt-in because it targets an already running
// local service. It verifies the complete browser path: HTTP authentication,
// WebSocket upgrade, application authentication, PTY/controller startup and
// the initial tmux layout message.
func TestLiveWebSocketLayout(t *testing.T) {
	url := os.Getenv("WEBTMUX_LIVE_WS")
	credential := os.Getenv("WEBTMUX_LIVE_CREDENTIAL")
	if url == "" || credential == "" {
		t.Skip("set WEBTMUX_LIVE_WS and WEBTMUX_LIVE_CREDENTIAL")
	}

	header := http.Header{}
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := conn.WriteJSON(InitMessage{AuthToken: credential}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte{webtty.SetEncoding, 'b', 'a', 's', 'e', '6', '4'}); err != nil {
		t.Fatal(err)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 1 && data[0] == webtty.TmuxLayoutUpdate {
			return
		}
	}
}
