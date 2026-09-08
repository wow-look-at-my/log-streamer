package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// stepFromEnv reads the section a stream belongs to from the runner's own
// environment, so a job whose steps share a token comes back split. It returns
// nil where nothing names a step, which keeps a plain local run unmarked.
func stepFromEnv() *protocol.Marker {
	m := protocol.Marker{
		Step:     firstEnv("LOG_STREAMER_STEP_ID", "GITHUB_ACTION"),
		Name:     firstEnv("LOG_STREAMER_STEP_NAME"),
		Job:      firstEnv("LOG_STREAMER_JOB", "GITHUB_JOB"),
		Workflow: firstEnv("LOG_STREAMER_WORKFLOW", "GITHUB_WORKFLOW"),
	}
	if m == (protocol.Marker{}) {
		return nil
	}
	return &m
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// step is a marked section of a stream.
type step struct {
	Index int
	Start protocol.Marker
	End   *protocol.Marker

	StartedAt time.Time
	EndedAt   time.Time
	Lines     []protocol.StreamMessage
}

// Matches reports whether a --step reference names this step. A reference is
// the position, the runner's step id, or the step's name.
func (s step) Matches(ref string) bool {
	if ref == "" {
		return true
	}
	if n, err := strconv.Atoi(ref); err == nil {
		return n == s.Index
	}
	return strings.EqualFold(ref, s.Start.Step) || strings.EqualFold(ref, s.Start.Name)
}

// Exit reports the step's status, and false while the step is still running.
func (s step) Exit() (int, bool) {
	if s.End == nil || s.End.Exit == nil {
		return 0, false
	}
	return *s.End.Exit, true
}

// groupSteps splits a log into its marked steps. Whatever precedes a start
// marker is preamble: a log with no markers at all is entirely preamble, which
// is what a stream that never went through the shell looks like.
func groupSteps(lines []protocol.StreamMessage) (steps []step, preamble []protocol.StreamMessage) {
	var current *step
	for _, line := range lines {
		m, ok := markerOf(line)
		if !ok {
			if current != nil {
				current.Lines = append(current.Lines, line)
			} else {
				preamble = append(preamble, line)
			}
			continue
		}
		switch m.Event {
		case protocol.EventStepStart:
			steps = append(steps, step{Index: len(steps) + 1, Start: m, StartedAt: line.Timestamp})
			current = &steps[len(steps)-1]
		case protocol.EventStepEnd:
			if current != nil {
				end := m
				current.End = &end
				current.EndedAt = line.Timestamp
				current = nil
			}
		}
	}
	return steps, preamble
}

// markerOf reads a line written to the marker stream.
func markerOf(line protocol.StreamMessage) (protocol.Marker, bool) {
	if line.Stream != protocol.StreamMarker.String() {
		return protocol.Marker{}, false
	}
	return protocol.ParseMarker(line.Line)
}

// writeStepIndex prints a row per step: what ran, how it ended, how long it
// took. It is the map a reader needs before asking for a step's output.
func writeStepIndex(out *bufio.Writer, steps []step, preamble int) {
	if len(steps) == 0 {
		fmt.Fprintf(out, "no steps recorded (%d lines)\n", preamble)
		fmt.Fprintln(out, "steps appear when the job streams through `ls-client shell`")
		return
	}
	for _, s := range steps {
		status := "running"
		if code, done := s.Exit(); done {
			status = "ok"
			if code != 0 {
				status = fmt.Sprintf("exit %d", code)
			}
		}
		took := ""
		if !s.EndedAt.IsZero() {
			took = fmt.Sprintf(" in %s", s.EndedAt.Sub(s.StartedAt).Round(time.Millisecond))
		}
		job := ""
		if s.Start.Job != "" {
			job = " [" + s.Start.Job + "]"
		}
		fmt.Fprintf(out, "%3d  %-9s %5d lines%s  %s%s\n",
			s.Index, status, len(s.Lines), took, s.Start.Label(), job)
	}
	if preamble > 0 {
		fmt.Fprintf(out, "     %-9s %5d lines  (outside any step)\n", "-", preamble)
	}
}
