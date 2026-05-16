package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/server"
)

var rootCmd = &cobra.Command{
	Use:   "log-streamer-server",
	Short: "Log streaming server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := server.ConfigFromEnv()
		srv, err := server.New(cfg)
		if err != nil {
			return err
		}
		return srv.Run()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
