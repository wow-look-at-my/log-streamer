package cmd

import (
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
	// A pipe gets the same guarantees a run does: the reader is never held up by
	// the socket, and a socket that breaks is reconnected and resumed.
	q := newQueue()
	sender := newDurableSender(q, func() (*websocket.Conn, protocol.ServerHello, error) {
		conn, err := dialStream(wsURL)
		if err != nil {
			return nil, protocol.ServerHello{}, err
		}
		var hello protocol.ServerHello
		if err := conn.ReadJSON(&hello); err != nil {
			conn.Close()
			return nil, protocol.ServerHello{}, err
		}
		return conn, hello, nil
	})
	go sender.run()
	go func() { announceToken(sender.awaitToken()) }()

	// Piped output gets its own section when the runner's env names a step.
	step := stepFromEnv()
	if step != nil {
		step.Event = protocol.EventStepStart
		sendMarker(sender, *step)
	}

	pumpErr := pump(os.Stdin, protocol.StreamStdin, os.Stdout, sender.sendFrame)

	// The end marker must reach the wire before the close, so it cannot be
	// deferred past it.
	if step != nil {
		done := 0
		step.Event, step.Exit = protocol.EventStepEnd, &done
		sendMarker(sender, *step)
	}
	q.Close()
	sender.wait()

	return pumpErr
}
