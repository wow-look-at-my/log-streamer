package cmd

import (
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// chunkSize bounds bytes read and sent per frame; memory stays O(chunkSize)
// regardless of a log line's length.
const chunkSize = 32 * 1024

// wsSender serializes binary frame writes to a shared connection; control
// writes stay safe to interleave.
type wsSender struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// sendFrame encodes and sends a frame; the payload is copied, so callers may
// reuse their buffer right after.
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
