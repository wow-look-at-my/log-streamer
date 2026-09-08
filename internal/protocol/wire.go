package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// StreamID identifies which output stream a frame belongs to.
type StreamID byte

const (
	StreamStdout StreamID = 0
	StreamStderr StreamID = 1
	StreamStdin  StreamID = 2
	// StreamMarker carries structure, not output: a JSON line per step boundary.
	StreamMarker StreamID = 3
)

func (s StreamID) String() string {
	switch s {
	case StreamStdout:
		return "stdout"
	case StreamStderr:
		return "stderr"
	case StreamStdin:
		return "stdin"
	case StreamMarker:
		return "marker"
	default:
		return fmt.Sprintf("stream%d", byte(s))
	}
}

// FrameHeaderSize is the wire frame prefix: a stream id plus a timestamp.
const FrameHeaderSize = 1 + 8

// ErrShortFrame is returned when a frame body is smaller than the header.
var ErrShortFrame = errors.New("frame too short")

// EncodeWire builds a frame body: [stream][timestamp][payload]. The payload
// is stored verbatim, so any bytes round-trip exactly and a chunk may be cut
// at any byte boundary.
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
