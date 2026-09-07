package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var runCmd = &cobra.Command{
	Use:   "run [command] [args...]",
	Short: "Run a command and stream its output to the server",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRun,
}

func init() {
	// Stop own-flag parsing at the child's positional arg, so a child flag
	// like `run make -j4` is not misread as ours.
	runCmd.Flags().SetInterspersed(false)
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
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

	pingDone := make(chan struct{})
	startPinger(conn, pingDone)
	defer close(pingDone)

	child := exec.Command(args[0], args[1:]...)
	child.Stdin = os.Stdin

	stdoutPipe, err := child.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := child.StderrPipe()
	if err != nil {
		return err
	}

	if err := child.Start(); err != nil {
		return err
	}

	sender := &wsSender{conn: conn}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = pump(stdoutPipe, protocol.StreamStdout, os.Stdout, sender.sendFrame)
	}()
	go func() {
		defer wg.Done()
		_ = pump(stderrPipe, protocol.StreamStderr, os.Stderr, sender.sendFrame)
	}()

	wg.Wait()
	exitErr := child.Wait()

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	if exitErr != nil {
		if exit, ok := exitErr.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return exitErr
	}
	return nil
}
