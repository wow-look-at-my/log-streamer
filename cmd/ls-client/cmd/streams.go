package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var streamsJSON bool

// errGroupNotFound marks a group the server holds no index for.
var errGroupNotFound = errors.New("group not found")

var streamsCmd = &cobra.Command{
	Use:   "streams [group]",
	Short: "List the streams indexed under a group token",
	Long: `List the streams indexed under a group token.

A job streams into a token derived from the key and its own name, which a
watcher cannot guess for a matrix leg. Each stream also registers under a group
token derived from the run alone, so this lists the legs and hands back the
token that fetches each one.

Pass the group, or pass the key and let it derive the group:

  ls-client streams --key "$KEY" --context owner/repo/12345/1
  ls-client fetch --follow <token from the listing>`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStreams,
}

func init() {
	streamsCmd.Flags().BoolVar(&streamsJSON, "json", false,
		"print the listing as JSON, for a script that fetches each stream")
	rootCmd.AddCommand(streamsCmd)
}

func runStreams(cmd *cobra.Command, args []string) error {
	group, err := groupArgOrDerived(args)
	if err != nil {
		return err
	}
	resp, err := fetchGroup(group)
	if err != nil {
		return err
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	if streamsJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if len(resp.Streams) == 0 {
		fmt.Fprintln(out, "no streams registered under this group yet")
		return nil
	}
	for _, m := range resp.Streams {
		label := m.Label
		if label == "" {
			label = "(unlabelled)"
		}
		fmt.Fprintf(out, "%s  %8s  %s  %s\n",
			m.Token, humanBytes(m.Bytes), m.LastSeen.Format(time.RFC3339), label)
	}
	return nil
}

func fetchGroup(group string) (protocol.GroupResponse, error) {
	var out protocol.GroupResponse

	resp, err := http.Get(getHTTPURL() + "/api/groups/" + group)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return out, errGroupNotFound
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

// humanBytes keeps a listing narrow enough to read a column at a time.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
