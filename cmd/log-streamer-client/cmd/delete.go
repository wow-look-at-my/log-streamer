package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <token>",
	Short: "Delete logs by token",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	tok := args[0]
	req, err := http.NewRequest(http.MethodDelete, getHTTPURL()+"/api/logs/"+tok, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		fmt.Println("deleted")
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	var errResp protocol.ErrorResponse
	if json.Unmarshal(body, &errResp) == nil {
		return fmt.Errorf("server: %s", errResp.Error)
	}
	return fmt.Errorf("server returned %d", resp.StatusCode)
}
