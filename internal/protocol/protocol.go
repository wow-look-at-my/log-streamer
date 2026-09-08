package protocol

import "time"

// StreamMessage is a reassembled log line returned by the fetch API. The wire
// and disk format is the compact binary frame in wire.go.
type StreamMessage struct {
	Timestamp time.Time `json:"ts"`
	Line      string    `json:"line"`
	Stream    string    `json:"stream"`
}

// ServerHello is the JSON text frame the server opens a stream connection
// with, carrying the freshly minted token.
//
// BytesStored is what the server already holds for this token, measured in the
// same record framing its file uses. A client that lost its connection resumes
// from exactly there: everything before it is stored and everything after it is
// not, so a reconnect neither drops a frame nor repeats it.
type ServerHello struct {
	Token       string `json:"token"`
	BytesStored int64  `json:"bytes_stored,omitempty"`
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
