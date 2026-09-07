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

var (
	serverURL   string
	streamToken string
)

var rootCmd = &cobra.Command{
	Use:   "log-streamer-client",
	Short: "Stream and retrieve logs from log-streamer-server",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "server URL (overrides LOG_STREAMER_SERVER env)")
}

// streamURL builds the stream endpoint, naming the stream when the caller
// chose a token. A caller that names its own stream can read the log back
// before the writer finishes, which is the point in CI.
func streamURL() (string, error) {
	base := getWSURL() + "/api/stream"
	tok := getStreamToken()
	if tok == "" {
		return base, nil
	}
	if !token.Validate(tok) {
		return "", fmt.Errorf("token must be 64 hex characters, got %q", tok)
	}
	return base + "?token=" + url.QueryEscape(tok), nil
}

// announceToken reports a server-minted token, which the caller has no other
// way to learn. A token the caller chose is already known, and echoing it would
// only park a live credential in the log.
func announceToken(tok string) {
	if getStreamToken() == "" {
		fmt.Fprintf(os.Stderr, "log-streamer token: %s\n", tok)
	}
}

func getStreamToken() string {
	if streamToken != "" {
		return streamToken
	}
	return os.Getenv("LOG_STREAMER_TOKEN")
}

// addStreamTokenFlag registers --token on a command that opens a stream.
func addStreamTokenFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&streamToken, "token", "",
		"stream into this 64-hex token instead of a server-generated one (overrides LOG_STREAMER_TOKEN env)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getWSURL() string {
	if serverURL != "" {
		return serverURL
	}
	if v := os.Getenv("LOG_STREAMER_SERVER"); v != "" {
		return v
	}
	return "ws://localhost:8080"
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
