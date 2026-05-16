package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var serverURL string

var rootCmd = &cobra.Command{
	Use:   "log-streamer-client",
	Short: "Stream and retrieve logs from log-streamer-server",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "server URL (overrides LOG_STREAMER_SERVER env)")
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
