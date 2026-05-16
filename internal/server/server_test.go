package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/testify/require"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := Config{
		Addr:		":0",
		DataDir:	t.TempDir(),
	}
	srv, err := New(cfg)
	require.Nil(t, err)

	return srv
}

func TestStreamAndFetch(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.Nil(t, err)

	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))

	require.Equal(t, 64, len(hello.Token))

	msg := protocol.StreamMessage{
		Timestamp:	time.Now().UTC(),
		Line:		"test line",
		Stream:		"stdout",
	}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	resp, err := http.Get(ts.URL + "/api/logs/" + hello.Token)
	require.Nil(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var fetchResp protocol.FetchResponse
	json.NewDecoder(resp.Body).Decode(&fetchResp)
	require.Equal(t, 1, fetchResp.Count)

	require.Equal(t, "test line", fetchResp.Lines[0].Line)

}

func TestFetchInvalidToken(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/logs/badtoken")
	require.Nil(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

}

func TestFetchNotFound(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	resp, err := http.Get(ts.URL + "/api/logs/" + tok)
	require.Nil(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)

}

func TestDeleteFlow(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.Nil(t, err)

	var hello protocol.ServerHello
	conn.ReadJSON(&hello)

	msg := protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "x", Stream: "stdout"}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)
	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/"+hello.Token, nil)
	resp, err := http.DefaultClient.Do(req)
	require.Nil(t, err)

	resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp2, _ := http.Get(ts.URL + "/api/logs/" + hello.Token)
	resp2.Body.Close()
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)

}

func TestDeleteNotFound(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/"+tok, nil)
	resp, err := http.DefaultClient.Do(req)
	require.Nil(t, err)

	resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)

}

func TestDeleteInvalidToken(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/short", nil)
	resp, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestConfigFromEnv(t *testing.T) {
	cfg := ConfigFromEnv()
	require.Equal(t, ":8080", cfg.Addr)
	require.Equal(t, "./data", cfg.DataDir)

	t.Setenv("LOG_STREAMER_ADDR", ":9090")
	t.Setenv("LOG_STREAMER_DATA_DIR", "/tmp/logs")
	cfg = ConfigFromEnv()
	require.Equal(t, ":9090", cfg.Addr)
	require.Equal(t, "/tmp/logs", cfg.DataDir)
}

func TestStreamInvalidJSON(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.Nil(t, err)

	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))

	conn.WriteMessage(websocket.TextMessage, []byte("not json"))

	msg := protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "valid", Stream: "stdout"}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	resp, err := http.Get(ts.URL + "/api/logs/" + hello.Token)
	require.Nil(t, err)
	defer resp.Body.Close()

	var fetchResp protocol.FetchResponse
	json.NewDecoder(resp.Body).Decode(&fetchResp)
	require.Equal(t, 1, fetchResp.Count)
}

func TestStreamWithPing(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.Nil(t, err)

	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))

	require.NoError(t, conn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(time.Second)))

	msg := protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "after ping", Stream: "stdout"}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	resp, err := http.Get(ts.URL + "/api/logs/" + hello.Token)
	require.Nil(t, err)
	defer resp.Body.Close()

	var fetchResp protocol.FetchResponse
	json.NewDecoder(resp.Body).Decode(&fetchResp)
	require.Equal(t, 1, fetchResp.Count)
	require.Equal(t, "after ping", fetchResp.Lines[0].Line)
}

func TestStreamMultipleLines(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.Nil(t, err)

	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))

	for i := 0; i < 10; i++ {
		msg := protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "line", Stream: "stderr"}
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)
	}

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	resp, err := http.Get(ts.URL + "/api/logs/" + hello.Token)
	require.Nil(t, err)
	defer resp.Body.Close()

	var fetchResp protocol.FetchResponse
	json.NewDecoder(resp.Body).Decode(&fetchResp)
	require.Equal(t, 10, fetchResp.Count)
}
