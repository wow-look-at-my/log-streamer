package server

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/storage"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	idleTimeout = 120 * time.Second

	// maxFrameBytes bounds an inbound message's in-memory buffer.
	maxFrameBytes = 1 << 20
)

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxFrameBytes)

	// A caller may name its own stream. CI needs this: a server-minted token
	// only ever reaches the caller over this socket, and a CI job's only way to
	// report it is its log, which the provider hides until the run ends.
	tok := r.URL.Query().Get("token")
	if tok != "" && !token.Validate(tok) {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid token format"))
		return
	}
	if tok == "" {
		var err error
		if tok, err = token.Generate(); err != nil {
			log.Printf("token generation: %v", err)
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "token generation failed"))
			return
		}
	}

	if err := conn.WriteJSON(protocol.ServerHello{Token: tok}); err != nil {
		log.Printf("write hello: %v", err)
		return
	}

	writer, err := s.store.OpenWriter(tok)
	if err != nil {
		log.Printf("open writer: %v", err)
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "storage error"))
		return
	}
	defer writer.Close()

	var bytesReceived int64
	conn.SetReadDeadline(time.Now().Add(idleTimeout))
	conn.SetPingHandler(func(appData string) error {
		conn.SetReadDeadline(time.Now().Add(idleTimeout))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			if websocket.IsUnexpectedCloseError(err) {
				break
			}
			log.Printf("read: %v", err)
			break
		}

		conn.SetReadDeadline(time.Now().Add(idleTimeout))

		// Log data arrives as binary frames; ignore anything else (a misbehaving
		// or probing client sending text/JSON).
		if mt != websocket.BinaryMessage || len(msg) < protocol.FrameHeaderSize {
			continue
		}

		if err := writer.Append(msg); err != nil {
			if errors.Is(err, storage.ErrStreamFull) || errors.Is(err, storage.ErrDiskFull) {
				conn.WriteJSON(protocol.ErrorResponse{Error: err.Error()})
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()))
				break
			}
			log.Printf("store append: %v", err)
			continue
		}
		bytesReceived += int64(len(msg))
	}

	conn.WriteJSON(protocol.ServerAck{BytesReceived: bytesReceived})

	log.Printf("stream %s: %d bytes", tok[:12], bytesReceived)
}
