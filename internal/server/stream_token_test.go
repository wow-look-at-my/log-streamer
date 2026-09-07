package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

const chosenToken = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0a1b2"

// storeSettleTimeout bounds the wait for a written frame to reach the store.
const storeSettleTimeout = 2 * time.Second

// dialStreamWith opens a stream naming its own token, as a CI job does.
func dialStreamWith(t *testing.T, ts *httptest.Server, query string) (*websocket.Conn, protocol.ServerHello) {
	t.Helper()
	wsURL := "ws" + ts.URL[4:] + "/api/stream" + query
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))
	return conn, hello
}

func TestStreamAcceptsCallerChosenToken(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStreamWith(t, ts, "?token="+chosenToken)
	require.Equal(t, chosenToken, hello.Token, "the server must honour the caller's token")
	sendFrame(t, conn, protocol.StreamStdout, "from ci\n")
	closeConn(t, conn)

	// The reader knew this token before the writer ever connected.
	resp, fr := fetch(t, ts, chosenToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 1, fr.Count)
	require.Equal(t, "from ci", fr.Lines[0].Line)
}

func TestStreamRejectsMalformedCallerToken(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream?token=../../etc/passwd"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "the upgrade succeeds; the server rejects on the socket")
	defer conn.Close()

	// A malformed token must close the stream rather than name a file with it.
	_, _, err = conn.ReadMessage()
	require.Error(t, err)
	require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation),
		"expected a policy-violation close, got %v", err)
}

func TestStreamStillMintsATokenWhenCallerOmitsOne(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStreamWith(t, ts, "")
	defer conn.Close()
	require.Len(t, hello.Token, 64)
	require.NotEqual(t, chosenToken, hello.Token)
}

// TestFetchMidStreamReturnsPartialOutput pins the property the live-CI use
// case rests on: a log reads back while its writer still holds the stream open.
func TestFetchMidStreamReturnsPartialOutput(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStreamWith(t, ts, "?token="+chosenToken)
	defer conn.Close()

	sendFrame(t, conn, protocol.StreamStdout, "early line\n")
	requireLineCount(t, ts, hello.Token, 1)

	sendFrame(t, conn, protocol.StreamStdout, "later line\n")
	requireLineCount(t, ts, hello.Token, 2)

	_, fr := fetch(t, ts, hello.Token)
	require.Equal(t, "early line", fr.Lines[0].Line)
	require.Equal(t, "later line", fr.Lines[1].Line)
}

func TestFetchSinceReturnsOnlyNewLines(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStreamWith(t, ts, "?token="+chosenToken)
	defer conn.Close()
	sendFrame(t, conn, protocol.StreamStdout, "alpha\nbeta\ngamma\n")
	requireLineCount(t, ts, hello.Token, 3)

	fr := fetchSince(t, ts, hello.Token, 2)
	require.Equal(t, 3, fr.Count, "Count stays the total so a follower can rewind")
	require.Len(t, fr.Lines, 1)
	require.Equal(t, "gamma", fr.Lines[0].Line)

	// A cursor at or past the end yields nothing, rather than an error.
	require.Empty(t, fetchSince(t, ts, hello.Token, 3).Lines)
	require.Empty(t, fetchSince(t, ts, hello.Token, 99).Lines)
}

func TestFetchRejectsBadSince(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStreamWith(t, ts, "?token="+chosenToken)
	defer conn.Close()
	sendFrame(t, conn, protocol.StreamStdout, "x\n")
	requireLineCount(t, ts, hello.Token, 1)

	for _, bad := range []string{"-1", "abc", "1.5"} {
		resp, err := http.Get(ts.URL + "/api/logs/" + hello.Token + "?since=" + bad)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, "since=%s must be refused", bad)
	}
}

func fetchSince(t *testing.T, ts *httptest.Server, tok string, since int) protocol.FetchResponse {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/logs/" + tok + "?since=" + strconv.Itoa(since))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var fr protocol.FetchResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&fr))
	return fr
}

// requireLineCount waits for a written frame to reach the store, since it
// crosses a socket and lands on disk on the server's own clock.
func requireLineCount(t *testing.T, ts *httptest.Server, tok string, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		resp, err := http.Get(ts.URL + "/api/logs/" + tok)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		var fr protocol.FetchResponse
		if json.NewDecoder(resp.Body).Decode(&fr) != nil {
			return false
		}
		return fr.Count == want
	}, storeSettleTimeout, storeSettleTimeout/100, "expected %d stored line(s)", want)
}
