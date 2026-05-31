package storage

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/testify/require"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func frame(stream protocol.StreamID, payload string) []byte {
	return protocol.EncodeWire(stream, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), []byte(payload))
}

func writeFrames(t *testing.T, store *Store, tok string, frames ...[]byte) {
	t.Helper()
	w, err := store.OpenWriter(tok)
	require.NoError(t, err)
	for _, f := range frames {
		require.NoError(t, w.Append(f))
	}
	require.NoError(t, w.Close())
}

func TestAppendAndFetch(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	writeFrames(t, store, testToken, frame(protocol.StreamStdout, "hello\n"))

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 1, len(lines))
	require.Equal(t, "hello", lines[0].Line)
	require.Equal(t, "stdout", lines[0].Stream)
}

func TestMultipleLinesInOneFrame(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	writeFrames(t, store, testToken, frame(protocol.StreamStdout, "a\nb\nc\n"))

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 3, len(lines))
	require.Equal(t, "a", lines[0].Line)
	require.Equal(t, "c", lines[2].Line)
}

func TestChunkedLineReassembly(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	// A single logical line split across three frames, completed by a newline.
	writeFrames(t, store, testToken,
		frame(protocol.StreamStdout, "abc"),
		frame(protocol.StreamStdout, "def"),
		frame(protocol.StreamStdout, "ghi\n"),
	)

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 1, len(lines))
	require.Equal(t, "abcdefghi", lines[0].Line)
}

func TestArbitrarilyLongLine(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	// 1 MiB of content with no newline until the very end, delivered in many
	// frames. This must round-trip as a single line.
	const total = 1 << 20
	big := strings.Repeat("x", total)
	w, err := store.OpenWriter(testToken)
	require.NoError(t, err)
	for off := 0; off < total; off += 4096 {
		end := off + 4096
		if end > total {
			end = total
		}
		require.NoError(t, w.Append(frame(protocol.StreamStdout, big[off:end])))
	}
	require.NoError(t, w.Append(frame(protocol.StreamStdout, "\n")))
	require.NoError(t, w.Close())

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 1, len(lines))
	require.Equal(t, total, len(lines[0].Line))
}

func TestTrailingLineWithoutNewline(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	writeFrames(t, store, testToken,
		frame(protocol.StreamStdout, "done\n"),
		frame(protocol.StreamStdout, "no newline here"),
	)

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 2, len(lines))
	require.Equal(t, "no newline here", lines[1].Line)
}

func TestInterleavedStreams(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	// A stdout line is in progress when a complete stderr line arrives; they
	// must not bleed into each other.
	writeFrames(t, store, testToken,
		frame(protocol.StreamStdout, "out-part1 "),
		frame(protocol.StreamStderr, "an error\n"),
		frame(protocol.StreamStdout, "out-part2\n"),
	)

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 2, len(lines))
	// stderr completed first.
	require.Equal(t, "stderr", lines[0].Stream)
	require.Equal(t, "an error", lines[0].Line)
	require.Equal(t, "stdout", lines[1].Stream)
	require.Equal(t, "out-part1 out-part2", lines[1].Line)
}

func TestBinaryPayloadPreserved(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	// Bytes that JSON-string storage would have mangled: NUL, an invalid UTF-8
	// byte, and an ESC. They must survive verbatim through the binary format.
	raw := string([]byte{0x00, 0xff, 0x1b, '[', '0', 'm', '\n'})
	writeFrames(t, store, testToken, frame(protocol.StreamStdout, raw))

	lines, err := store.Fetch(testToken)
	require.NoError(t, err)
	require.Equal(t, 1, len(lines))
	require.Equal(t, raw[:len(raw)-1], lines[0].Line) // without the trailing newline
}

func TestPerStreamCap(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir(), MaxStreamBytes: 12})
	require.NoError(t, err)

	w, err := store.OpenWriter(testToken)
	require.NoError(t, err)
	defer w.Close()

	// Each frame body is FrameHeaderSize(9)+payload. "ab" => 11 bytes, ok.
	require.NoError(t, w.Append(frame(protocol.StreamStdout, "ab")))
	// Next 11-byte frame would push total to 22 > 12.
	require.ErrorIs(t, w.Append(frame(protocol.StreamStdout, "cd")), ErrStreamFull)
}

func TestTotalCap(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir(), MaxTotalBytes: 14})
	require.NoError(t, err)

	w, err := store.OpenWriter(testToken)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Append(frame(protocol.StreamStdout, "ab")))
	require.ErrorIs(t, w.Append(frame(protocol.StreamStdout, "cd")), ErrDiskFull)
}

func TestDelete(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	writeFrames(t, store, testToken, frame(protocol.StreamStdout, "x\n"))
	require.True(t, store.Exists(testToken))
	require.NoError(t, store.Delete(testToken))
	require.False(t, store.Exists(testToken))
}

func TestFetchNotFound(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	_, err = store.Fetch(testToken)
	require.True(t, os.IsNotExist(err))
}

func TestDeleteNotFound(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	err = store.Delete(testToken)
	require.True(t, os.IsNotExist(err))
}

func TestBadToken(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)

	_, err = store.OpenWriter("../escape")
	require.ErrorIs(t, err, ErrBadToken)
	_, err = store.Fetch("not-a-token")
	require.ErrorIs(t, err, ErrBadToken)
	require.ErrorIs(t, store.Delete("nope"), ErrBadToken)
	require.False(t, store.Exists("nope"))
}

func TestTotalBytesAccounting(t *testing.T) {
	dir := t.TempDir()
	store, err := New(Options{Dir: dir})
	require.NoError(t, err)
	require.Equal(t, int64(0), store.TotalBytes())

	writeFrames(t, store, testToken, frame(protocol.StreamStdout, "hello\n"))
	require.Greater(t, store.TotalBytes(), int64(0))

	// A fresh store over the same dir must recover the same usage from disk.
	store2, err := New(Options{Dir: dir})
	require.NoError(t, err)
	require.Equal(t, store.TotalBytes(), store2.TotalBytes())

	require.NoError(t, store.Delete(testToken))
	require.Equal(t, int64(0), store.TotalBytes())
}

func TestSweep(t *testing.T) {
	dir := t.TempDir()
	store, err := New(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)

	writeFrames(t, store, testToken, frame(protocol.StreamStdout, "old\n"))

	// Age the file beyond the TTL.
	old := time.Now().Add(-2 * time.Hour)
	path, err := store.filePath(testToken)
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(path, old, old))

	n, err := store.Sweep(time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.False(t, store.Exists(testToken))
}

func TestSweepDisabled(t *testing.T) {
	store, err := New(Options{Dir: t.TempDir(), TTL: 0})
	require.NoError(t, err)
	writeFrames(t, store, testToken, frame(protocol.StreamStdout, "keep\n"))

	n, err := store.Sweep(time.Now())
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.True(t, store.Exists(testToken))
}
