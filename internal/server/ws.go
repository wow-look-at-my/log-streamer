package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const idleTimeout = 120 * time.Second

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	defer conn.Close()

	tok, err := token.Generate()
	if err != nil {
		log.Printf("token generation: %v", err)
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "token generation failed"))
		return
	}

	hello := protocol.ServerHello{Token: tok}
	if err := conn.WriteJSON(hello); err != nil {
		log.Printf("write hello: %v", err)
		return
	}

	var linesReceived int
	conn.SetReadDeadline(time.Now().Add(idleTimeout))
	conn.SetPingHandler(func(appData string) error {
		conn.SetReadDeadline(time.Now().Add(idleTimeout))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	for {
		_, msg, err := conn.ReadMessage()
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

		var sm protocol.StreamMessage
		if err := json.Unmarshal(msg, &sm); err != nil {
			continue
		}

		if err := s.store.Append(tok, sm); err != nil {
			log.Printf("store append: %v", err)
			continue
		}
		linesReceived++
	}

	ack := protocol.ServerAck{LinesReceived: linesReceived}
	conn.WriteJSON(ack)

	log.Printf("stream %s: %d lines", tok[:12], linesReceived)
}
