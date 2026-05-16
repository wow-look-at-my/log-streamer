package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

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

	var mu sync.Mutex
	send := func(line, stream string) {
		msg := protocol.StreamMessage{
			Timestamp: time.Now().UTC(),
			Line:      line,
			Stream:    stream,
		}
		data, _ := json.Marshal(msg)
		mu.Lock()
		conn.WriteMessage(websocket.TextMessage, data)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println(line)
			send(line, "stdout")
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(os.Stderr, line)
			send(line, "stderr")
		}
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
