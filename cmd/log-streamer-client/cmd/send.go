package cmd

import (
	"fmt"
	"os"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Read stdin and stream to the server",
	RunE:  runSend,
}

func init() {
	addStreamTokenFlag(sendCmd)
	rootCmd.AddCommand(sendCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	wsURL, err := streamURL()
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	defer conn.Close()

	var hello protocol.ServerHello
	if err := conn.ReadJSON(&hello); err != nil {
		return fmt.Errorf("reading token: %w", err)
	}
	fmt.Fprintf(os.Stderr, "log-streamer token: %s\n", hello.Token)

	pingDone := make(chan struct{})
	startPinger(conn, pingDone)
	defer close(pingDone)

	sender := &wsSender{conn: conn}
	pumpErr := pump(os.Stdin, protocol.StreamStdin, os.Stdout, sender.sendFrame)

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	return pumpErr
}
