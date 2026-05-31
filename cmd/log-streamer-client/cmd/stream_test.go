package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/testify/require"
)

func TestPumpSmallInput(t *testing.T) {
	input := []byte("hello\nworld\n")
	var local bytes.Buffer
	var sent []byte
	var gotStream protocol.StreamID

	err := pump(bytes.NewReader(input), protocol.StreamStdout, &local,
		func(stream protocol.StreamID, _ time.Time, payload []byte) error {
			gotStream = stream
			sent = append(sent, payload...) // copy out of the reusable buffer
			return nil
		})

	require.NoError(t, err)
	require.Equal(t, protocol.StreamStdout, gotStream)
	require.Equal(t, input, sent)
	require.Equal(t, input, local.Bytes())
}

func TestPumpChunksLargeInput(t *testing.T) {
	// Larger than chunkSize and with no newline at all: must still stream out
	// in full, across multiple frames, with bounded per-frame size.
	input := []byte(strings.Repeat("a", chunkSize*3+17))
	var sent []byte
	frames := 0

	err := pump(bytes.NewReader(input), protocol.StreamStderr, nil,
		func(_ protocol.StreamID, _ time.Time, payload []byte) error {
			frames++
			require.LessOrEqual(t, len(payload), chunkSize)
			sent = append(sent, payload...)
			return nil
		})

	require.NoError(t, err)
	require.Equal(t, input, sent)
	require.GreaterOrEqual(t, frames, 4)
}

func TestSanitizeControl(t *testing.T) {
	require.Equal(t, "plain text", sanitizeControl("plain text"))
	require.Equal(t, "keep\ttab", sanitizeControl("keep\ttab"))
	require.Equal(t, "esc^[here", sanitizeControl("esc\x1bhere"))
	require.Equal(t, "bell^G", sanitizeControl("bell\x07"))
	require.Equal(t, "del^?", sanitizeControl("del\x7f"))

	// A C1 control (NEL, U+0085) must be neutralized (not emitted verbatim).
	// Built via string(rune(...)) to keep this source file pure ASCII.
	nel := string(rune(0x85))
	got := sanitizeControl("x" + nel + "y")
	require.NotContains(t, got, nel)
	require.Contains(t, got, "0085")
}

func TestGetURLs(t *testing.T) {
	orig := serverURL
	defer func() { serverURL = orig }()

	serverURL = ""
	t.Setenv("LOG_STREAMER_SERVER", "")
	require.Equal(t, "ws://localhost:8080", getWSURL())
	require.Equal(t, "http://localhost:8080", getHTTPURL())

	serverURL = "wss://logs.example.com"
	require.Equal(t, "wss://logs.example.com", getWSURL())
	require.Equal(t, "https://logs.example.com", getHTTPURL())

	serverURL = ""
	t.Setenv("LOG_STREAMER_SERVER", "ws://from-env:9000")
	require.Equal(t, "ws://from-env:9000", getWSURL())
	require.Equal(t, "http://from-env:9000", getHTTPURL())
}

func TestIsTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "regular")
	require.NoError(t, err)
	defer f.Close()
	require.False(t, isTerminal(f))

	if dn, err := os.Open(os.DevNull); err == nil {
		defer dn.Close()
		require.True(t, isTerminal(dn)) // /dev/null is a character device
	}
}
