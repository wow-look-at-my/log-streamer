package protocol

import "time"

type StreamMessage struct {
	Timestamp time.Time `json:"ts"`
	Line      string    `json:"line"`
	Stream    string    `json:"stream"`
}

type ServerHello struct {
	Token string `json:"token"`
}

type ServerAck struct {
	LinesReceived int `json:"lines_received"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type FetchResponse struct {
	Token string          `json:"token"`
	Lines []StreamMessage `json:"lines"`
	Count int             `json:"count"`
}
