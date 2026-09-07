package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var fetchRaw bool

var fetchCmd = &cobra.Command{
	Use:   "fetch <token>",
	Short: "Retrieve logs by token",
	Args:  cobra.ExactArgs(1),
	RunE:  runFetch,
}

func init() {
	fetchCmd.Flags().BoolVar(&fetchRaw, "raw", false,
		"print log content verbatim, without escaping terminal control sequences")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(cmd *cobra.Command, args []string) error {
	tok := args[0]
	resp, err := http.Get(getHTTPURL() + "/api/logs/" + tok)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var errResp protocol.ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("server: %s", errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var fetchResp protocol.FetchResponse
	if err := json.Unmarshal(body, &fetchResp); err != nil {
		return err
	}

	// Untrusted log content: escape control sequences on a terminal only.
	sanitize := !fetchRaw && isTerminal(os.Stdout)

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for _, line := range fetchResp.Lines {
		text := line.Line
		if sanitize {
			text = sanitizeControl(text)
		}
		fmt.Fprintf(out, "[%s] [%s] %s\n",
			line.Timestamp.Format("2006-01-02T15:04:05Z07:00"), line.Stream, text)
	}
	return nil
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
