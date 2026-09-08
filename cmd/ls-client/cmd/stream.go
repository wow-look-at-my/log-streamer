package cmd

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// chunkSize bounds bytes read and sent per frame.
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

// flushInterval bounds the wait for a newline, so a prompt still gets through.
const flushInterval = 200 * time.Millisecond

// readResult carries what a read returned, so the pump can wait on the reader
// and on the flush timer at the same time.
type readResult struct {
	data []byte
	err  error
}

// pump reads r, tees the raw bytes to local (best effort), and sends them as
// binary frames on line boundaries. A frame carries whole lines when they are
// available. A partial line is sent anyway when flushInterval passes, and a
// line longer than chunkSize is sent in pieces, which bounds memory. The
// timestamp is when the bytes were read, not when they were sent. send must
// not retain the payload after it returns.
func pump(r io.Reader, stream protocol.StreamID, local io.Writer, send func(protocol.StreamID, time.Time, []byte) error) error {
	reads := make(chan readResult)
	go func() {
		defer close(reads)
		for {
			buf := make([]byte, chunkSize)
			n, err := r.Read(buf)
			reads <- readResult{data: buf[:n], err: err}
			if err != nil {
				return
			}
		}
	}()

	var pending []byte
	var pendingAt time.Time

	timer := time.NewTimer(flushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	armed := false

	flush := func(upTo int) error {
		if upTo == 0 {
			return nil
		}
		if err := send(stream, pendingAt, pending[:upTo]); err != nil {
			return err
		}
		pending = append(pending[:0], pending[upTo:]...)
		if len(pending) == 0 && armed {
			if !timer.Stop() {
				<-timer.C
			}
			armed = false
		}
		return nil
	}

	for {
		select {
		case res, ok := <-reads:
			if !ok {
				return nil
			}
			if len(res.data) > 0 {
				if local != nil {
					_, _ = local.Write(res.data)
				}
				if len(pending) == 0 {
					pendingAt = time.Now().UTC()
				}
				pending = append(pending, res.data...)

				switch {
				case bytes.LastIndexByte(pending, '\n') >= 0:
					if err := flush(bytes.LastIndexByte(pending, '\n') + 1); err != nil {
						return err
					}
				case len(pending) >= chunkSize:
					if err := flush(len(pending)); err != nil {
						return err
					}
				}
				if len(pending) > 0 && !armed {
					timer.Reset(flushInterval)
					armed = true
				}
			}
			if res.err != nil {
				if err := flush(len(pending)); err != nil {
					return err
				}
				if res.err == io.EOF {
					return nil
				}
				return res.err
			}

		case <-timer.C:
			// The newline never came, so the reader gets the partial line.
			armed = false
			if err := flush(len(pending)); err != nil {
				return err
			}
		}
	}
}
