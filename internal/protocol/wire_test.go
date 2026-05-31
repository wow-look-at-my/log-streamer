package protocol

import (
	"testing"
	"time"

	"github.com/wow-look-at-my/testify/require"
)

func TestEncodeDecodeWireRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 31, 12, 0, 0, 123456789, time.UTC)
	payload := []byte{0x00, 0xff, 'h', 'i', '\n', 0x1b}

	body := EncodeWire(StreamStderr, ts, payload)
	stream, gotTS, gotPayload, err := DecodeWire(body)
	require.NoError(t, err)
	require.Equal(t, StreamStderr, stream)
	require.True(t, gotTS.Equal(ts))
	require.Equal(t, payload, gotPayload)
}

func TestDecodeWireShortFrame(t *testing.T) {
	_, _, _, err := DecodeWire([]byte{0x00, 0x01})
	require.ErrorIs(t, err, ErrShortFrame)
}

func TestStreamIDString(t *testing.T) {
	require.Equal(t, "stdout", StreamStdout.String())
	require.Equal(t, "stderr", StreamStderr.String())
	require.Equal(t, "stdin", StreamStdin.String())
	require.Equal(t, "stream7", StreamID(7).String())
}

func TestEncodeWireEmptyPayload(t *testing.T) {
	body := EncodeWire(StreamStdout, time.Unix(0, 0), nil)
	require.Equal(t, FrameHeaderSize, len(body))
	stream, _, payload, err := DecodeWire(body)
	require.NoError(t, err)
	require.Equal(t, StreamStdout, stream)
	require.Equal(t, 0, len(payload))
}
