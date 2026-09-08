package cmd

import (
	"bufio"
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// markerLine builds the line a marker occupies after the store reassembles it.
func markerLine(t *testing.T, m protocol.Marker, at time.Time) protocol.StreamMessage {
	t.Helper()
	encoded, err := protocol.EncodeMarker(m)
	require.NoError(t, err)
	return protocol.StreamMessage{
		Timestamp: at,
		Line:      string(encoded[:len(encoded)-1]),
		Stream:    protocol.StreamMarker.String(),
	}
}

func outLine(text string, at time.Time) protocol.StreamMessage {
	return protocol.StreamMessage{Timestamp: at, Line: text, Stream: "stdout"}
}

// jobLog is a job whose steps streamed into a single token.
func jobLog(t *testing.T) []protocol.StreamMessage {
	t.Helper()
	base := time.Unix(1700000000, 0).UTC()
	ok, failed := 0, 1
	return []protocol.StreamMessage{
		outLine("runner boot", base),
		markerLine(t, protocol.Marker{Event: protocol.EventStepStart, Step: "__run", Cmd: "make build", Job: "test"}, base.Add(time.Second)),
		outLine("compiling", base.Add(2*time.Second)),
		markerLine(t, protocol.Marker{Event: protocol.EventStepEnd, Step: "__run", Cmd: "make build", Exit: &ok}, base.Add(3*time.Second)),
		markerLine(t, protocol.Marker{Event: protocol.EventStepStart, Step: "__run_2", Name: "Tests", Job: "test"}, base.Add(4*time.Second)),
		outLine("FAIL", base.Add(5*time.Second)),
		markerLine(t, protocol.Marker{Event: protocol.EventStepEnd, Step: "__run_2", Name: "Tests", Exit: &failed}, base.Add(6*time.Second)),
	}
}

func TestGroupStepsSplitsAJobIntoItsSteps(t *testing.T) {
	steps, preamble := groupSteps(jobLog(t))

	require.Len(t, preamble, 1, "output before a start marker belongs to no step")
	require.Len(t, steps, 2)

	require.Equal(t, "make build", steps[0].Start.Label())
	require.Equal(t, []string{"compiling"}, linesOf(steps[0]))
	code, done := steps[0].Exit()
	require.True(t, done)
	require.Zero(t, code)
	require.Equal(t, 2*time.Second, steps[0].EndedAt.Sub(steps[0].StartedAt),
		"a step is timed by its own markers")

	require.Equal(t, "Tests", steps[1].Start.Label())
	code, done = steps[1].Exit()
	require.True(t, done)
	require.Equal(t, 1, code)
}

// A stream nobody watched to the end has a step still open.
func TestGroupStepsLeavesARunningStepUnfinished(t *testing.T) {
	at := time.Unix(1700000000, 0).UTC()
	steps, _ := groupSteps([]protocol.StreamMessage{
		markerLine(t, protocol.Marker{Event: protocol.EventStepStart, Step: "__run"}, at),
		outLine("still going", at.Add(time.Second)),
	})

	require.Len(t, steps, 1)
	_, done := steps[0].Exit()
	require.False(t, done)
	require.Equal(t, []string{"still going"}, linesOf(steps[0]))
}

// A log with no markers at all reads back whole, so an older writer still works.
func TestGroupStepsKeepsAnUnmarkedLogIntact(t *testing.T) {
	at := time.Unix(1700000000, 0).UTC()
	steps, preamble := groupSteps([]protocol.StreamMessage{outLine("plain", at)})

	require.Empty(t, steps)
	require.Len(t, preamble, 1)
}

func TestStepMatchesByPositionIdOrName(t *testing.T) {
	s := step{Index: 2, Start: protocol.Marker{Step: "__run_2", Name: "Tests"}}

	require.True(t, s.Matches(""), "no reference means every step")
	require.True(t, s.Matches("2"))
	require.True(t, s.Matches("__run_2"))
	require.True(t, s.Matches("tests"), "a name matches whatever its case")
	require.False(t, s.Matches("1"))
	require.False(t, s.Matches("Build"))
}

func TestWriteStepIndexReportsStatusAndSize(t *testing.T) {
	steps, preamble := groupSteps(jobLog(t))

	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	writeStepIndex(out, steps, len(preamble))
	require.NoError(t, out.Flush())

	got := buf.String()
	require.Contains(t, got, "make build")
	require.Contains(t, got, "ok")
	require.Contains(t, got, "exit 1")
	require.Contains(t, got, "Tests")
	require.Contains(t, got, "outside any step")
}

func TestWriteStepIndexSaysWhereStepsComeFrom(t *testing.T) {
	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	writeStepIndex(out, nil, 4)
	require.NoError(t, out.Flush())

	require.Contains(t, buf.String(), "no steps recorded")
	require.Contains(t, buf.String(), "ls-client shell")
}

// The runner names the step; the client reads that naming off the environment.
func TestStepFromEnv(t *testing.T) {
	t.Setenv("GITHUB_ACTION", "")
	t.Setenv("GITHUB_JOB", "")
	t.Setenv("GITHUB_WORKFLOW", "")
	t.Setenv("LOG_STREAMER_STEP_NAME", "")
	t.Setenv("LOG_STREAMER_STEP_ID", "")
	t.Setenv("LOG_STREAMER_JOB", "")
	t.Setenv("LOG_STREAMER_WORKFLOW", "")
	require.Nil(t, stepFromEnv(), "nothing names a step outside a runner")

	t.Setenv("GITHUB_ACTION", "__run_2")
	t.Setenv("GITHUB_JOB", "test")
	t.Setenv("GITHUB_WORKFLOW", "CI")
	got := stepFromEnv()
	require.NotNil(t, got)
	require.Equal(t, "__run_2", got.Step)
	require.Equal(t, "test", got.Job)
	require.Equal(t, "CI", got.Workflow)
	require.Empty(t, got.Name)

	t.Setenv("LOG_STREAMER_STEP_NAME", "Tests")
	require.Equal(t, "Tests", stepFromEnv().Name)
}

func linesOf(s step) []string {
	var out []string
	for _, line := range s.Lines {
		out = append(out, line.Line)
	}
	return out
}
