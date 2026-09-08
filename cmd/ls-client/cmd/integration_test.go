package cmd

import (
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/server"
)

// globalStateMu serializes tests mutating shared package state (serverURL, os.Stdout).
var globalStateMu sync.Mutex

func lockGlobalState(t *testing.T) {
	t.Helper()
	globalStateMu.Lock()
	t.Cleanup(globalStateMu.Unlock)
}

func startClientTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, err := server.New(server.Config{DataDir: t.TempDir()})
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func pointClientAt(t *testing.T, ts *httptest.Server) {
	t.Helper()
	orig := serverURL
	serverURL = "ws://" + strings.TrimPrefix(ts.URL, "http://")
	t.Cleanup(func() { serverURL = orig })
}

// captureStdout redirects os.Stdout to a temp file for the duration of a test,
// so command output does not pollute the test log and writers never block.
func captureStdout(t *testing.T) {
	t.Helper()
	orig := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	os.Stdout = f
	t.Cleanup(func() {
		os.Stdout = orig
		f.Close()
	})
}

func streamOneFrame(t *testing.T, ts *httptest.Server, payload string) string {
	t.Helper()
	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/api/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	var hello protocol.ServerHello
	require.NoError(t, conn.ReadJSON(&hello))

	body := protocol.EncodeWire(protocol.StreamStdout, time.Now().UTC(), []byte(payload))
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, body))
	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(50 * time.Millisecond)
	return hello.Token
}

func TestClientFetchAndDelete(t *testing.T) {
	lockGlobalState(t)
	ts := startClientTestServer(t)
	pointClientAt(t, ts)
	captureStdout(t)

	tok := streamOneFrame(t, ts, "hello from client test\n")

	require.NoError(t, runFetch(fetchCmd, []string{tok}))
	require.NoError(t, runDelete(deleteCmd, []string{tok}))
	// Deleting a now-missing stream surfaces the server's error.
	require.Error(t, runDelete(deleteCmd, []string{tok}))
}

func TestClientFetchInvalidToken(t *testing.T) {
	lockGlobalState(t)
	ts := startClientTestServer(t)
	pointClientAt(t, ts)
	captureStdout(t)

	require.Error(t, runFetch(fetchCmd, []string{"too-short"}))
}

func TestClientSend(t *testing.T) {
	lockGlobalState(t)
	ts := startClientTestServer(t)
	pointClientAt(t, ts)
	captureStdout(t)

	origStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()
	go func() {
		io.WriteString(w, "alpha\nbeta\n")
		w.Close()
	}()

	require.NoError(t, runSend(sendCmd, nil))
}

func TestClientRun(t *testing.T) {
	lockGlobalState(t)
	ts := startClientTestServer(t)
	pointClientAt(t, ts)
	captureStdout(t)

	// Exits cleanly, writes to both stdout and stderr, so both pumps run.
	require.NoError(t, runRun(runCmd, []string{"sh", "-c", "echo out; echo err 1>&2"}))
}

func TestClientRunViaCLIWithChildFlags(t *testing.T) {
	lockGlobalState(t)
	ts := startClientTestServer(t)
	captureStdout(t)

	// Drive the real cobra path: the child's `-c` flag must reach sh, not us.
	host := strings.TrimPrefix(ts.URL, "http://")
	origURL := serverURL
	rootCmd.SetArgs([]string{"--server", "ws://" + host, "run", "sh", "-c", "echo viaCLI"})
	err := rootCmd.Execute()
	rootCmd.SetArgs(nil)
	serverURL = origURL
	require.NoError(t, err)
}

func TestClientRunConnectError(t *testing.T) {
	lockGlobalState(t)
	orig := serverURL
	serverURL = "ws://127.0.0.1:1" // nothing listening
	defer func() { serverURL = orig }()
	require.Error(t, runRun(runCmd, []string{"echo", "hi"}))
}
