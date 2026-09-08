package cmd

import (
	"fmt"
	"os"

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
	conn, err := dialStream(wsURL)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	defer conn.Close()

	var hello protocol.ServerHello
	if err := conn.ReadJSON(&hello); err != nil {
		return fmt.Errorf("reading token: %w", err)
	}
	announceToken(hello.Token)

	pingDone := make(chan struct{})
	startPinger(conn, pingDone)
	defer close(pingDone)

	sender := &wsSender{conn: conn}

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
	closeStream(conn)

	return pumpErr
}
