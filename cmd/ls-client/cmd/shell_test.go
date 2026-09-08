package cmd

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	// The runner writes a step's script to a file and passes the path.
	path := filepath.Join(t.TempDir(), "step")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestFirstCommandSkipsBlanksAndComments(t *testing.T) {
	require.Equal(t, "make build",
		firstCommand(writeScript(t, "\n# a comment\n\nmake build\nmake test\n")))
	require.Empty(t, firstCommand(writeScript(t, "# nothing but a comment\n")))
	require.Empty(t, firstCommand(filepath.Join(t.TempDir(), "absent")))
}

func TestFirstCommandTruncatesALongOpeningLine(t *testing.T) {
	got := firstCommand(writeScript(t, strings.Repeat("x", maxCmdLabel+40)+"\n"))
	require.True(t, strings.HasSuffix(got, "..."))
	require.Len(t, got, maxCmdLabel+len("..."))
}

func TestShellCommandMatchesTheFlagsActionsUses(t *testing.T) {
	t.Setenv("LOG_STREAMER_SHELL", "")
	got := shellCommand()
	require.Contains(t, strings.Join(got, " "), "-o pipefail",
		"a step must fail when a command inside a pipeline fails")
	require.Contains(t, strings.Join(got, " "), "-e")

	t.Setenv("LOG_STREAMER_SHELL", "/bin/dash")
	require.Equal(t, []string{"/bin/dash"}, shellCommand())
}

// A job's steps stream into one token, and the log comes back split by step.
func TestShellStreamsAStepAndFetchSplitsItBack(t *testing.T) {
	lockGlobalState(t)
	captureStdout(t)
	ts := startClientTestServer(t)
	pointClientAt(t, ts)

	t.Setenv("LOG_STREAMER_SHELL", "")
	t.Setenv("GITHUB_ACTION", "__run_2")
	t.Setenv("GITHUB_JOB", "test")
	t.Setenv("GITHUB_WORKFLOW", "CI")
	t.Setenv("LOG_STREAMER_STEP_NAME", "")

	// The job names its own stream, which is how its steps share a token.
	tok, err := token.Generate()
	require.NoError(t, err)
	t.Setenv("LOG_STREAMER_TOKEN", tok)

	rootCmd.SetArgs([]string{"shell", writeScript(t, "echo hello from the step\n")})
	require.NoError(t, rootCmd.Execute())

	resp, err2 := fetchSince(tok, 0)
	require.NoError(t, err2)

	steps, preamble := groupSteps(resp.Lines)
	require.Empty(t, preamble)
	require.Len(t, steps, 1)
	require.Equal(t, "echo hello from the step", steps[0].Start.Label(),
		"Actions exports no step name, so the opening command labels the step")
	require.Equal(t, "test", steps[0].Start.Job)
	code, done := steps[0].Exit()
	require.True(t, done)
	require.Zero(t, code)
	require.Contains(t, linesOf(steps[0]), "hello from the step")

	// --step prints that step and nothing else.
	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	r := &renderer{out: out, stepRef: "1"}
	r.write(resp.Lines)
	require.NoError(t, out.Flush())
	require.Contains(t, buf.String(), "hello from the step")
	require.Contains(t, buf.String(), "=== step 1:")

	// A reference naming another step leaves only its own output out.
	buf.Reset()
	out = bufio.NewWriter(&buf)
	r = &renderer{out: out, stepRef: "nope"}
	r.write(resp.Lines)
	require.NoError(t, out.Flush())
	require.NotContains(t, buf.String(), "hello from the step")
}

// --raw is what a caller pipes elsewhere, so structure must stay out of it.
func TestRendererKeepsMarkersOutOfRawOutput(t *testing.T) {
	at := jobLog(t)

	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	r := &renderer{out: out, raw: true}
	r.write(at)
	require.NoError(t, out.Flush())

	require.Contains(t, buf.String(), "compiling")
	require.NotContains(t, buf.String(), "=== step")
	require.NotContains(t, buf.String(), protocol.EventStepStart)
}

func TestRendererRewindsWhenALogRestarts(t *testing.T) {
	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	r := &renderer{out: out}
	r.write(jobLog(t))
	r.reset()
	r.write(jobLog(t))
	require.NoError(t, out.Flush())

	require.Equal(t, 2, strings.Count(buf.String(), "=== step 1:"),
		"a restarted log numbers its steps from the top again")
}
