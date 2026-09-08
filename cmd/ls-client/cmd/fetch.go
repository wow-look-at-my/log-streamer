package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var (
	fetchRaw      bool
	fetchFollow   bool
	fetchInterval time.Duration
)

// errStreamNotFound marks a token the server holds no log for.
var errStreamNotFound = errors.New("stream not found")

var fetchCmd = &cobra.Command{
	Use:   "fetch <token>",
	Short: "Retrieve logs by token",
	Args:  cobra.ExactArgs(1),
	RunE:  runFetch,
}

func init() {
	fetchCmd.Flags().BoolVar(&fetchRaw, "raw", false,
		"print log content verbatim, without escaping terminal control sequences")
	fetchCmd.Flags().BoolVarP(&fetchFollow, "follow", "f", false,
		"keep polling and print new lines as they arrive, until interrupted")
	fetchCmd.Flags().DurationVar(&fetchInterval, "interval", time.Second,
		"how often --follow polls for new lines")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(cmd *cobra.Command, args []string) error {
	tok := args[0]
	// Untrusted log content: escape control sequences on a terminal only.
	sanitize := !fetchRaw && isTerminal(os.Stdout)

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	if !fetchFollow {
		resp, err := fetchSince(tok, 0)
		if err != nil {
			return err
		}
		writeLines(out, resp.Lines, sanitize)
		return nil
	}
	return followStream(cmd.Context(), out, tok, sanitize)
}

// followStream prints new lines on a fixed cadence until the caller interrupts.
// A log still being written reads back fine, so this trails a live stream.
func followStream(ctx context.Context, out *bufio.Writer, tok string, sanitize bool) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(fetchInterval)
	defer ticker.Stop()

	cursor := 0
	waiting := false
	for {
		resp, err := fetchSince(tok, cursor)
		switch {
		case errors.Is(err, errStreamNotFound):
			// A watcher can compute the token before the build reaches the step
			// that streams, so an absent log means "not yet", never "give up".
			if !waiting {
				fmt.Fprintf(os.Stderr, "waiting for stream %s\n", tok)
				waiting = true
			}
		case err != nil:
			return err
		case resp.Count < cursor:
			// Shorter than the cursor means restarted, so re-read from the top.
			cursor = 0
			continue
		default:
			writeLines(out, resp.Lines, sanitize)
			if err := out.Flush(); err != nil {
				return err
			}
			cursor += len(resp.Lines)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// fetchSince reads the log from a line offset, plus the total count.
func fetchSince(tok string, since int) (protocol.FetchResponse, error) {
	var out protocol.FetchResponse

	target := getHTTPURL() + "/api/logs/" + tok
	if since > 0 {
		target += "?since=" + strconv.Itoa(since)
	}
	resp, err := http.Get(target)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return out, errStreamNotFound
	}
	if resp.StatusCode != http.StatusOK {
		var errResp protocol.ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return out, fmt.Errorf("server: %s", errResp.Error)
		}
		return out, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func writeLines(out *bufio.Writer, lines []protocol.StreamMessage, sanitize bool) {
	for _, line := range lines {
		text := line.Line
		if sanitize {
			text = sanitizeControl(text)
		}
		fmt.Fprintf(out, "[%s] [%s] %s\n",
			line.Timestamp.Format("2006-01-02T15:04:05Z07:00"), line.Stream, text)
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// sanitizeControl renders control characters as visible, inert text. Reassembled
// line content never contains '\n' (it is the line delimiter), and tabs are kept.
func sanitizeControl(s string) string {
	if !strings.ContainsFunc(s, isUnsafeControl) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteRune(r)
		case r == 0x7f:
			b.WriteString("^?")
		case r < 0x20:
			b.WriteByte('^')
			b.WriteRune(r + '@')
		case unicode.IsControl(r):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isUnsafeControl(r rune) bool {
	return r != '\t' && unicode.IsControl(r)
}
