package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

// The org's server, so an ordinary client needs no flag and no env var.
const defaultServerURL = "wss://logs.pazer.io"

var (
	serverURL   string
	streamToken string
	streamGroup string
)

var rootCmd = &cobra.Command{
	Use:   "ls-client",
	Short: "Stream and retrieve logs from ls-server",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "",
		"server URL (overrides LOG_STREAMER_SERVER env; default "+defaultServerURL+")")

	// Every command that names a stream derives it, so nothing has to compute
	// the value beforehand and carry it around.
	rootCmd.PersistentFlags().StringVar(&deriveKey, "key", "",
		"shared derivation key (overrides LOG_STREAMER_STREAM_KEY env)")
	rootCmd.PersistentFlags().StringVar(&deriveContext, "context", "",
		"context to derive from (default: repository/run-id/run-attempt/job from the GitHub Actions env)")
	rootCmd.PersistentFlags().StringVar(&deriveName, "name", "",
		"extra context separating streams within a job, such as a matrix leg (overrides LOG_STREAMER_NAME env)")
}

// streamURL builds the stream endpoint, naming the stream when the caller
// chose a token. A caller that names its own stream can read the log back
// before the writer finishes, which is the point in CI. A group indexes the
// stream, so a watcher holding the key can list it without knowing its name.
func streamURL() (string, error) {
	base := getWSURL() + "/api/stream"
	tok := getStreamToken()
	if tok == "" {
		return base, nil
	}
	if !token.Validate(tok) {
		return "", fmt.Errorf("token must be 64 hex characters, got %q", tok)
	}

	q := url.Values{"token": {tok}}
	if group := getStreamGroup(); group != "" {
		if !token.Validate(group) {
			return "", fmt.Errorf("group must be 64 hex characters, got %q", group)
		}
		q.Set("group", group)
		if label := getStreamLabel(); label != "" {
			q.Set("label", label)
		}
	}
	return base + "?" + q.Encode(), nil
}

// dialStream opens the stream socket. A plaintext dial that a server answers
// with a redirect to TLS is retried over wss, because a WebSocket handshake
// cannot follow a redirect. The retry says so on stderr.
func dialStream(wsURL string) (*websocket.Conn, error) {
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		return conn, nil
	}
	if !strings.HasPrefix(wsURL, "ws://") || resp == nil || resp.StatusCode < 300 || resp.StatusCode > 399 {
		return nil, err
	}
	secure := "wss://" + strings.TrimPrefix(wsURL, "ws://")
	fmt.Fprintf(os.Stderr, "log-streamer: %s redirects to TLS, retrying over wss\n", wsURL)
	conn, _, err = websocket.DefaultDialer.Dial(secure, nil)
	return conn, err
}

// announceToken reports only a minted token. Echoing a caller's own token back
// tells it nothing and parks a live credential in the log.
func announceToken(tok string) {
	if getStreamToken() == "" {
		fmt.Fprintf(os.Stderr, "log-streamer token: %s\n", tok)
	}
}

// getStreamToken names the stream to write into. A key is enough on its own:
// the client derives the same token the watcher does, so nothing has to
// compute it beforehand and no workflow ever holds the value.
func getStreamToken() string {
	if streamToken != "" {
		return streamToken
	}
	if tok := os.Getenv("LOG_STREAMER_TOKEN"); tok != "" {
		return tok
	}
	tok, err := token.Derive(derivationKey(), derivedContext())
	if err != nil {
		return ""
	}
	return tok
}

// tokenArgOrDerived reads the token a command works on, preferring what the
// caller named over what the key derives.
func tokenArgOrDerived(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if tok := getStreamToken(); tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("no token: pass one, or pass --key (or set LOG_STREAMER_STREAM_KEY) to derive it")
}

// groupArgOrDerived reads the group a listing works on.
func groupArgOrDerived(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if group := getStreamGroup(); group != "" {
		return group, nil
	}
	return "", fmt.Errorf("no group: pass one, or pass --key (or set LOG_STREAMER_STREAM_KEY) to derive it")
}

// getStreamGroup names the index this stream registers in, derived from the
// same key so a watcher lists the run without knowing any leg's name.
func getStreamGroup() string {
	if streamGroup != "" {
		return streamGroup
	}
	if group := os.Getenv("LOG_STREAMER_GROUP"); group != "" {
		return group
	}
	group, err := token.DeriveGroup(derivationKey(), derivedGroupContext())
	if err != nil {
		return ""
	}
	return group
}

// getStreamLabel names this stream in its group's listing.
func getStreamLabel() string {
	return firstEnv("LOG_STREAMER_LABEL", "GITHUB_JOB")
}

// addStreamTokenFlag registers --token and --group on a command that opens a
// stream.
func addStreamTokenFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&streamToken, "token", "",
		"stream into this 64-hex token instead of a server-generated one (overrides LOG_STREAMER_TOKEN env)")
	cmd.Flags().StringVar(&streamGroup, "group", "",
		"index this stream under this 64-hex group token, which `streams` lists (overrides LOG_STREAMER_GROUP env)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// configuredServer is what the caller asked for, in whatever scheme.
func configuredServer() string {
	if serverURL != "" {
		return serverURL
	}
	if v := os.Getenv("LOG_STREAMER_SERVER"); v != "" {
		return v
	}
	return defaultServerURL
}

// getWSURL speaks ws or wss, the only schemes a WebSocket dial accepts. A
// bare host gets wss, because a public server redirects http to https and
// the dial fails on the redirect.
func getWSURL() string {
	s := configuredServer()
	switch {
	case strings.HasPrefix(s, "https://"):
		return "wss://" + strings.TrimPrefix(s, "https://")
	case strings.HasPrefix(s, "http://"):
		return "ws://" + strings.TrimPrefix(s, "http://")
	case strings.HasPrefix(s, "ws://"), strings.HasPrefix(s, "wss://"):
		return s
	}
	return "wss://" + s
}

func getHTTPURL() string {
	ws := getWSURL()
	ws = strings.Replace(ws, "wss://", "https://", 1)
	ws = strings.Replace(ws, "ws://", "http://", 1)
	return ws
}

func startPinger(conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			}
		}
	}()
}
