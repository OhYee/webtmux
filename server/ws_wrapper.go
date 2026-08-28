package server

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

type wsWrapper struct {
	*websocket.Conn
}

func (wsw *wsWrapper) Write(p []byte) (n int, err error) {
	if err := wsw.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return 0, err
	}
	writer, err := wsw.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return 0, err
	}
	defer writer.Close()
	return writer.Write(p)
}

func (wsw *wsWrapper) Read(p []byte) (n int, err error) {
	for {
		msgType, reader, err := wsw.Conn.NextReader()
		if err != nil {
			return 0, err
		}

		if msgType != websocket.TextMessage {
			continue
		}

		b, err := io.ReadAll(io.LimitReader(reader, int64(len(p)+1)))
		if len(b) > len(p) {
			return 0, errors.New("client message exceeded buffer size")
		}
		n = copy(p, b)
		return n, err
	}
}
