package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// redialInterval paces reconnects. It is fixed and it never gives up: a stream
// that stops retrying loses the rest of the run, which is the whole failure this
// sender exists to prevent. A longer wait would only make the gap longer.
const redialInterval = time.Second

// durableSender drains a spool onto a connection it owns, and replaces that
// connection whenever it breaks. It is the only writer, so what the server
// receives is what the spool holds, in the order it was appended.
type durableSender struct {
	spool *spool
	dial  func() (*websocket.Conn, protocol.ServerHello, error)

	// sent is how far into the spool the server is known to hold, in record
	// bytes. It is what a fresh connection's BytesStored is compared against.
	sent int64
	done chan struct{}

	conn     *websocket.Conn
	pingDone chan struct{}
	token    string
	tokenC   chan string
}

// drop closes the current connection and its pinger, so the next dial starts clean.
func (d *durableSender) drop() {
	if d.pingDone != nil {
		close(d.pingDone)
		d.pingDone = nil
	}
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
}

func newDurableSender(sp *spool, dial func() (*websocket.Conn, protocol.ServerHello, error)) *durableSender {
	return &durableSender{spool: sp, dial: dial, done: make(chan struct{}), tokenC: make(chan string, 1)}
}

// run drains the spool until it is closed and empty, then returns. It is the
// only place that blocks on the network, and by the time it is waited on the
// command has already exited.
func (d *durableSender) run() {
	defer close(d.done)

	offset := int64(0)
	for {
		if d.conn == nil {
			if !d.connect(&offset) {
				return
			}
		}

		frame, next, ok := d.spool.next(offset)
		if !ok {
			d.finish()
			return
		}

		if err := d.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			// The frame is still in the spool and the offset has not moved, so the
			// reconnect resends exactly what the server did not store.
			fmt.Fprintf(os.Stderr, "log-streamer: the stream dropped, reconnecting: %v\n", err)
			d.drop()
			continue
		}
		offset = next
		d.sent = next
		d.spool.advance(next)
	}
}

// connect dials until it succeeds, then lines the spool position up with what
// the server already holds. Returns false only when the spool is finished and
// there is nothing left worth connecting for.
func (d *durableSender) connect(offset *int64) bool {
	for {
		if d.spool.pending() == 0 && d.spool.isClosed() {
			return false
		}
		conn, hello, err := d.dial()
		if err != nil {
			time.Sleep(redialInterval)
			continue
		}
		d.conn = conn
		// A quiet command still has to hold the connection open: the server drops an
		// idle one, and every drop costs a reconnect the run did not need.
		d.pingDone = make(chan struct{})
		startPinger(conn, d.pingDone)
		if d.token == "" {
			d.token = hello.Token
			select {
			case d.tokenC <- hello.Token:
			default:
			}
		}
		// The server's own count decides where to resume. Trusting the client's
		// idea instead is what turns a dropped frame into a silent hole, or a
		// retried one into a duplicate.
		if hello.BytesStored < *offset {
			*offset = hello.BytesStored
			d.spool.advance(hello.BytesStored)
		}
		return true
	}
}

// finish closes the stream and waits for the server to say what it stored. A
// mismatch is reported rather than swallowed: it is the one signal that says
// the log is not what the run produced.
func (d *durableSender) finish() {
	if d.conn == nil {
		return
	}
	closeStream(d.conn)
	d.drop()
}

// sendFrame encodes a frame and hands it to the spool. It never touches the
// network, so it never blocks the reader that calls it.
func (d *durableSender) sendFrame(stream protocol.StreamID, ts time.Time, payload []byte) error {
	return d.spool.Append(protocol.EncodeWire(stream, ts, payload))
}

// wait blocks until the spool is drained onto the wire. This is the tail
// guarantee, and it runs after the child has exited.
func (d *durableSender) wait() { <-d.done }

// awaitToken gives the caller the stream's name as soon as the first connection
// reports it.
func (d *durableSender) awaitToken() string {
	select {
	case tok := <-d.tokenC:
		return tok
	case <-d.done:
		return d.token
	}
}
