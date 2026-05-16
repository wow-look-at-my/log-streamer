package storage

import (
	"os"
	"testing"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/testify/require"
)

func TestAppendAndFetch(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	require.Nil(t, err)

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	msg := protocol.StreamMessage{
		Timestamp:	time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Line:		"hello",
		Stream:		"stdout",
	}

	require.NoError(t, store.Append(tok, msg))

	lines, err := store.Fetch(tok)
	require.Nil(t, err)

	require.Equal(t, 1, len(lines))

	require.Equal(t, "hello", lines[0].Line)

	require.Equal(t, "stdout", lines[0].Stream)

}

func TestMultipleAppends(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	require.Nil(t, err)

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for i := 0; i < 5; i++ {
		msg := protocol.StreamMessage{
			Timestamp:	time.Now().UTC(),
			Line:		"line",
			Stream:		"stdout",
		}
		require.NoError(t, store.Append(tok, msg))

	}

	lines, err := store.Fetch(tok)
	require.Nil(t, err)

	require.Equal(t, 5, len(lines))

}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	require.Nil(t, err)

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	msg := protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "x", Stream: "stdin"}
	store.Append(tok, msg)

	require.True(t, store.Exists(tok))

	require.NoError(t, store.Delete(tok))

	require.False(t, store.Exists(tok))

}

func TestFetchNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	require.Nil(t, err)

	_, err = store.Fetch("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.True(t, os.IsNotExist(err))

}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	require.Nil(t, err)

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	require.False(t, store.Exists(tok))

	store.Append(tok, protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "x", Stream: "stdout"})
	require.True(t, store.Exists(tok))

}
