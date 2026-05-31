package cmd

import (
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// chunkSize bounds how many bytes are read and sent per frame. Memory use stays
// O(chunkSize) regardless of how long any single log line is.
const chunkSize = 32 * 1024

// wsSender serializes binary writes to one WebSocket connection so the stdout
// and stderr pumps can share it. Control writes (pings) are safe to issue
// concurrently with these per the gorilla/websocket contract.
type wsSender struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// sendFrame encodes and sends one frame. The payload is copied during encoding,
// so the caller may reuse its buffer immediately after this returns.
func (s *wsSender) sendFrame(stream protocol.StreamID, ts time.Time, payload []byte) error {
	body := protocol.EncodeWire(stream, ts, payload)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(websocket.BinaryMessage, body)
}

// pump reads r in bounded chunks, tees the raw bytes to local (best effort),
// and sends each chunk verbatim as a binary frame. It does not interpret '\n';
// line boundaries are reconstructed at fetch time. send must not retain the
// payload after it returns.
func pump(r io.Reader, stream protocol.StreamID, local io.Writer, send func(protocol.StreamID, time.Time, []byte) error) error {
	buf := make([]byte, chunkSize)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if local != nil {
				_, _ = local.Write(buf[:n])
			}
			if err := send(stream, time.Now().UTC(), buf[:n]); err != nil {
				return err
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}
