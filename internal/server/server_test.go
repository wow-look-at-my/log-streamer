package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.DataDir = t.TempDir()
	srv, err := New(cfg)
	require.NoError(t, err)
	return srv
}

func dialStream(t *testing.T, ts *httptest.Server) (*websocket.Conn, protocol.ServerHello) {
	t.Helper()
	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))
	return conn, hello
}

func sendFrame(t *testing.T, conn *websocket.Conn, stream protocol.StreamID, payload string) {
	t.Helper()
	body := protocol.EncodeWire(stream, time.Now().UTC(), []byte(payload))
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, body))
}

func closeConn(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()
}

func fetch(t *testing.T, ts *httptest.Server, tok string) (*http.Response, protocol.FetchResponse) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/logs/" + tok)
	require.NoError(t, err)
	defer resp.Body.Close()
	var fr protocol.FetchResponse
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&fr))
	}
	return resp, fr
}

func TestStreamAndFetch(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	require.Equal(t, 64, len(hello.Token))
	sendFrame(t, conn, protocol.StreamStdout, "test line\n")
	closeConn(t, conn)

	resp, fr := fetch(t, ts, hello.Token)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 1, fr.Count)
	require.Equal(t, "test line", fr.Lines[0].Line)
	require.Equal(t, "stdout", fr.Lines[0].Stream)
}

func TestStreamMultipleLines(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	for i := 0; i < 10; i++ {
		sendFrame(t, conn, protocol.StreamStderr, "line\n")
	}
	closeConn(t, conn)

	_, fr := fetch(t, ts, hello.Token)
	require.Equal(t, 10, fr.Count)
	require.Equal(t, "stderr", fr.Lines[0].Stream)
}

func TestStreamLongLineAcrossFrames(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	chunk := strings.Repeat("z", 40000)
	sendFrame(t, conn, protocol.StreamStdout, chunk)
	sendFrame(t, conn, protocol.StreamStdout, chunk)
	sendFrame(t, conn, protocol.StreamStdout, "\n")
	closeConn(t, conn)

	_, fr := fetch(t, ts, hello.Token)
	require.Equal(t, 1, fr.Count)
	require.Equal(t, 80000, len(fr.Lines[0].Line))
}

func TestStreamWithPing(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	require.NoError(t, conn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(time.Second)))
	sendFrame(t, conn, protocol.StreamStdout, "after ping\n")
	closeConn(t, conn)

	_, fr := fetch(t, ts, hello.Token)
	require.Equal(t, 1, fr.Count)
	require.Equal(t, "after ping", fr.Lines[0].Line)
}

func TestStreamIgnoresTextMessages(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	// A stray text/JSON message must be ignored, not stored.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"line":"nope"}`)))
	sendFrame(t, conn, protocol.StreamStdout, "real\n")
	closeConn(t, conn)

	_, fr := fetch(t, ts, hello.Token)
	require.Equal(t, 1, fr.Count)
	require.Equal(t, "real", fr.Lines[0].Line)
}

func TestStreamPerStreamCap(t *testing.T) {
	srv := testServer(t, Config{MaxStreamBytes: 20})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	sendFrame(t, conn, protocol.StreamStdout, "first\n") // 15 bytes body, ok
	// This frame pushes over the cap; the server should reject and close.
	sendFrame(t, conn, protocol.StreamStdout, "second line that is too big\n")

	// Drain until the connection is closed by the server.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	conn.Close()

	_, fr := fetch(t, ts, hello.Token)
	require.Equal(t, 1, fr.Count)
	require.Equal(t, "first", fr.Lines[0].Line)
}

func TestFetchInvalidToken(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, _ := fetch(t, ts, "badtoken")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestFetchNotFound(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	tok := strings.Repeat("0123456789abcdef", 4)
	resp, _ := fetch(t, ts, tok)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeleteFlow(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	conn, hello := dialStream(t, ts)
	sendFrame(t, conn, protocol.StreamStdout, "x\n")
	closeConn(t, conn)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/"+hello.Token, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp2, _ := fetch(t, ts, hello.Token)
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

func TestDeleteNotFound(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	tok := strings.Repeat("0123456789abcdef", 4)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/"+tok, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeleteInvalidToken(t *testing.T) {
	srv := testServer(t, Config{})
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/short", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestConfigFromEnv(t *testing.T) {
	cfg := ConfigFromEnv()
	require.Equal(t, ":8080", cfg.Addr)
	require.Equal(t, "./data", cfg.DataDir)
	require.Equal(t, int64(defaultMaxStreamBytes), cfg.MaxStreamBytes)
	require.Equal(t, int64(0), cfg.MaxTotalBytes)
	require.Equal(t, defaultTTL, cfg.TTL)

	t.Setenv("LOG_STREAMER_ADDR", ":9090")
	t.Setenv("LOG_STREAMER_DATA_DIR", "/tmp/logs")
	t.Setenv("LOG_STREAMER_MAX_STREAM_BYTES", "4096")
	t.Setenv("LOG_STREAMER_MAX_TOTAL_BYTES", "8192")
	t.Setenv("LOG_STREAMER_TTL", "1h")
	t.Setenv("LOG_STREAMER_SWEEP_INTERVAL", "5m")
	cfg = ConfigFromEnv()
	require.Equal(t, ":9090", cfg.Addr)
	require.Equal(t, "/tmp/logs", cfg.DataDir)
	require.Equal(t, int64(4096), cfg.MaxStreamBytes)
	require.Equal(t, int64(8192), cfg.MaxTotalBytes)
	require.Equal(t, time.Hour, cfg.TTL)
	require.Equal(t, 5*time.Minute, cfg.SweepInterval)
}

func TestConfigFromEnvInvalidFallsBack(t *testing.T) {
	t.Setenv("LOG_STREAMER_MAX_STREAM_BYTES", "not-a-number")
	t.Setenv("LOG_STREAMER_TTL", "not-a-duration")
	cfg := ConfigFromEnv()
	require.Equal(t, int64(defaultMaxStreamBytes), cfg.MaxStreamBytes)
	require.Equal(t, defaultTTL, cfg.TTL)
}
