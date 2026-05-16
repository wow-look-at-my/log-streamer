package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := Config{
		Addr:    ":0",
		DataDir: t.TempDir(),
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestStreamAndFetch(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	var hello protocol.ServerHello
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if len(hello.Token) != 64 {
		t.Fatalf("expected 64-char token, got %d", len(hello.Token))
	}

	msg := protocol.StreamMessage{
		Timestamp: time.Now().UTC(),
		Line:      "test line",
		Stream:    "stdout",
	}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	resp, err := http.Get(ts.URL + "/api/logs/" + hello.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var fetchResp protocol.FetchResponse
	json.NewDecoder(resp.Body).Decode(&fetchResp)
	if fetchResp.Count != 1 {
		t.Fatalf("expected 1 line, got %d", fetchResp.Count)
	}
	if fetchResp.Lines[0].Line != "test line" {
		t.Fatalf("expected 'test line', got %q", fetchResp.Lines[0].Line)
	}
}

func TestFetchInvalidToken(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/logs/badtoken")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFetchNotFound(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	resp, err := http.Get(ts.URL + "/api/logs/" + tok)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteFlow(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}

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
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	resp2, _ := http.Get(ts.URL + "/api/logs/" + hello.Token)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp2.StatusCode)
	}
}

func TestDeleteNotFound(t *testing.T) {
	srv := testServer(t)
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/logs/"+tok, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
