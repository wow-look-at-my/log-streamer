package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <token>",
	Short: "Retrieve logs by token",
	Args:  cobra.ExactArgs(1),
	RunE:  runFetch,
}

func init() {
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
		if json.Unmarshal(body, &errResp) == nil {
			return fmt.Errorf("server: %s", errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var fetchResp protocol.FetchResponse
	if err := json.Unmarshal(body, &fetchResp); err != nil {
		return err
	}

	for _, line := range fetchResp.Lines {
		fmt.Printf("[%s] [%s] %s\n", line.Timestamp.Format("2006-01-02T15:04:05Z"), line.Stream, line.Line)
	}
	return nil
}
