package protocol

import "time"

// StreamMessage is a single reassembled log line in a FetchResponse. It is the
// JSON shape returned by the fetch HTTP API; log data on the wire and on disk
// uses the compact binary frame format in wire.go.
type StreamMessage struct {
	Timestamp time.Time `json:"ts"`
	Line      string    `json:"line"`
	Stream    string    `json:"stream"`
}

// ServerHello is the first message the server sends on a stream connection
// (JSON text frame), carrying the freshly minted token.
type ServerHello struct {
	Token string `json:"token"`
}

// ServerAck is the final message the server sends on a stream connection
// (JSON text frame), reporting how many bytes it accepted.
type ServerAck struct {
	BytesReceived int64 `json:"bytes_received"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type FetchResponse struct {
	Token string          `json:"token"`
	Lines []StreamMessage `json:"lines"`
	Count int             `json:"count"`
}
