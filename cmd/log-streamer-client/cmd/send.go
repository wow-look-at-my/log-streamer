package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

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
	rootCmd.AddCommand(sendCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	conn, _, err := websocket.DefaultDialer.Dial(getWSURL()+"/api/stream", nil)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	defer conn.Close()

	var hello protocol.ServerHello
	if err := conn.ReadJSON(&hello); err != nil {
		return fmt.Errorf("reading token: %w", err)
	}
	fmt.Fprintf(os.Stderr, "log-streamer token: %s\n", hello.Token)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
		msg := protocol.StreamMessage{
			Timestamp: time.Now().UTC(),
			Line:      line,
			Stream:    "stdin",
		}
		data, _ := json.Marshal(msg)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return fmt.Errorf("sending: %w", err)
		}
	}

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	return scanner.Err()
}
