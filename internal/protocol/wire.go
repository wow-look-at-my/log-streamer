package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// StreamID identifies which output stream a frame belongs to. It is encoded as
// a single byte on the wire and on disk.
type StreamID byte

const (
	StreamStdout StreamID = 0
	StreamStderr StreamID = 1
	StreamStdin  StreamID = 2
)

func (s StreamID) String() string {
	switch s {
	case StreamStdout:
		return "stdout"
	case StreamStderr:
		return "stderr"
	case StreamStdin:
		return "stdin"
	default:
		return fmt.Sprintf("stream%d", byte(s))
	}
}

// FrameHeaderSize is the fixed prefix of a wire frame body: a 1-byte stream id
// followed by an 8-byte big-endian Unix-nanosecond timestamp. The payload (raw
// log bytes) follows and runs to the end of the frame.
const FrameHeaderSize = 1 + 8

// ErrShortFrame is returned when a frame body is smaller than the header.
var ErrShortFrame = errors.New("frame too short")

// EncodeWire builds a frame body: [stream:1][ts:8][payload...]. The payload is
// stored verbatim, so any bytes (text or binary) round-trip exactly and a chunk
// may be cut at any byte boundary.
func EncodeWire(stream StreamID, ts time.Time, payload []byte) []byte {
	b := make([]byte, FrameHeaderSize+len(payload))
	b[0] = byte(stream)
	binary.BigEndian.PutUint64(b[1:FrameHeaderSize], uint64(ts.UnixNano()))
	copy(b[FrameHeaderSize:], payload)
	return b
}

// DecodeWire parses a frame body. The returned payload slice aliases body and
// must not be retained past body's lifetime.
func DecodeWire(body []byte) (stream StreamID, ts time.Time, payload []byte, err error) {
	if len(body) < FrameHeaderSize {
		return 0, time.Time{}, nil, ErrShortFrame
	}
	stream = StreamID(body[0])
	ns := int64(binary.BigEndian.Uint64(body[1:FrameHeaderSize]))
	ts = time.Unix(0, ns).UTC()
	payload = body[FrameHeaderSize:]
	return stream, ts, payload, nil
}
