package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// streamExec runs a command with its output streamed and teed locally. A step
// wraps the output in markers, so a reader can split a job's log back apart.
// It exits with the command's status: a wrapper that swallows that hides a
// failure.
func streamExec(args []string, step *protocol.Marker) error {
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
	if step != nil {
		step.Event = protocol.EventStepStart
		sendMarker(sender, *step)
	}

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

	code := 0
	if exitErr != nil {
		if exit, ok := exitErr.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = -1
		}
	}
	if step != nil {
		end := *step
		end.Event = protocol.EventStepEnd
		end.Exit = &code
		sendMarker(sender, end)
	}

	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	if exitErr != nil {
		if _, ok := exitErr.(*exec.ExitError); ok {
			conn.Close()
			os.Exit(code)
		}
		return exitErr
	}
	return nil
}

// sendMarker reports a failure to stderr and continues. A lost boundary costs
// a reader a section header; it must never fail the step it wraps.
func sendMarker(s *wsSender, m protocol.Marker) {
	payload, err := protocol.EncodeMarker(m)
	if err == nil {
		err = s.sendFrame(protocol.StreamMarker, time.Now().UTC(), payload)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "log-streamer: step marker not sent: %v\n", err)
	}
}
