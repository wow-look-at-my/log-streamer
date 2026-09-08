package cmd

import (
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [command] [args...]",
	Short: "Run a command and stream its output to the server",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRun,
}

func init() {
	// Stop own-flag parsing at the child's arg, so `run make -j4` isn't misread.
	runCmd.Flags().SetInterspersed(false)
	addStreamTokenFlag(runCmd)
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	return streamExec(args, stepFromEnv())
}
