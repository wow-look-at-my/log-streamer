package cmd

import (
	"bufio"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

var shellCmd = &cobra.Command{
	Use:   "shell <script>",
	Short: "Run a script file as a GitHub Actions custom shell, marking step boundaries",
	Long: `Run a script file the way a GitHub Actions shell does, streaming it.

Set it as the job's shell and every run step streams into the same token:

  defaults:
    run:
      shell: ls-client shell {0}

The runner writes each step's script to a file and passes the path here. Each
step is its own process, so markers wrap each step and ` + "`fetch --steps`" + `
splits the job's log back apart.

Actions exports no step name, so a section is labelled with the command the
step opens with. Set LOG_STREAMER_STEP_NAME on a step to label it yourself.`,
	Args: cobra.ExactArgs(1),
	RunE: runShell,
}

func init() {
	shellCmd.Flags().SetInterspersed(false)
	addStreamTokenFlag(shellCmd)
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	// A shell step is a step even outside Actions, where the env names nothing.
	step := stepFromEnv()
	if step == nil {
		step = &protocol.Marker{}
	}
	step.Cmd = firstCommand(args[0])
	return streamExec(append(shellCommand(), args[0]), step)
}

// firstCommand reads the line a script opens with, skipping blanks and
// comments. Actions exports no step name, so this is what tells sections apart.
func firstCommand(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > maxCmdLabel {
			return line[:maxCmdLabel] + "..."
		}
		return line
	}
	return ""
}

// maxCmdLabel keeps a step index readable when a step opens with a long line.
const maxCmdLabel = 60

// shellCommand is the interpreter and the flags Actions itself gives a bash
// step: fail on an error, and fail on a failure anywhere in a pipeline. A
// runner without bash falls back to sh, which is what the runner does.
func shellCommand() []string {
	if cmd := os.Getenv("LOG_STREAMER_SHELL"); cmd != "" {
		return []string{cmd}
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return []string{path, "--noprofile", "--norc", "-e", "-o", "pipefail"}
	}
	return []string{"sh", "-e"}
}
