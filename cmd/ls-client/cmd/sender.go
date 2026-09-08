package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// redialInterval paces reconnects. It is fixed and it never gives up: a stream
// that stops retrying loses the rest of the run.
const redialInterval = time.Second

// durableSender drains a queue onto a connection it owns and replaces that
// connection when it breaks. It is the only writer, so what the server receives
// is what was produced, in that order.
type durableSender struct {
	queue *queue
	dial  func() (*websocket.Conn, protocol.ServerHello, error)

	// stored is what the server holds, in the record framing its file uses. A
	// reconnect reads the same number off the new connection, which is how a
	// frame that landed is told from one that did not.
	stored int64
	done   chan struct{}

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

func newDurableSender(q *queue, dial func() (*websocket.Conn, protocol.ServerHello, error)) *durableSender {
	return &durableSender{queue: q, dial: dial, done: make(chan struct{}), tokenC: make(chan string, 1)}
}

// run drains the queue until it is closed and empty. It is the only place that
// blocks on the network, and the command has exited before anything waits on it.
func (d *durableSender) run() {
	defer close(d.done)

	for {
		frame, ok := d.queue.Head()
		if !ok {
			d.finish()
			return
		}
		if d.conn == nil {
			d.connect()
		}
		if err := d.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			// The frame stays at the head, so the reconnect decides whether the server
			// got it and sends it again if not.
			fmt.Fprintf(os.Stderr, "log-streamer: the stream dropped, reconnecting: %v\n", err)
			d.drop()
			continue
		}
		d.stored += recordLen(frame)
		d.queue.Pop()
	}
}

// recordLen is what a frame occupies once stored, header included: the units the
// server counts in.
func recordLen(frame []byte) int64 {
	n, v := int64(1), uint64(len(frame))
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n + int64(len(frame))
}

// connect dials until it succeeds, then reconciles against what the server
// already holds. A frame this sender wrote that the server does not have stays
// at the head and goes again; one it does have is dropped, so a reconnect never
// writes the same frame twice.
func (d *durableSender) connect() {
	for {
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
		if head, ok := d.queue.Head(); ok && hello.BytesStored >= d.stored+recordLen(head) {
			d.queue.Pop()
		}
		d.stored = hello.BytesStored
		return
	}
}

// finish closes the stream and waits for the server to say what it stored.
func (d *durableSender) finish() {
	if d.conn == nil {
		return
	}
	closeStream(d.conn)
	d.drop()
}

// sendFrame encodes a frame and queues it. It never touches the network.
func (d *durableSender) sendFrame(stream protocol.StreamID, ts time.Time, payload []byte) error {
	d.queue.Push(protocol.EncodeWire(stream, ts, payload))
	return nil
}

// wait blocks until the queue is drained onto the wire. This is the tail
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
