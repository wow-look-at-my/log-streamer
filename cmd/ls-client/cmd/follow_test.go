package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

// followBudget bounds every wait here; reaching it means the follow stalled.
const followBudget = 2 * time.Second

// syncBuffer carries followed output between the follow and test goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// lineServer serves a growing log, standing in for a build still running.
type lineServer struct {
	mu    sync.Mutex
	lines []protocol.StreamMessage
	found bool
}

func (l *lineServer) line(text string) protocol.StreamMessage {
	return protocol.StreamMessage{
		Timestamp: time.Unix(0, 0).UTC(),
		Line:      text,
		Stream:    "stdout",
	}
}

func (l *lineServer) append(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, l.line(text))
}

func (l *lineServer) restartWith(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines[:0], l.line(text))
}

func (l *lineServer) setFound(found bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.found = found
}

func (l *lineServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if !l.found {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(protocol.ErrorResponse{Error: "token not found"})
			return
		}

		since, _ := strconv.Atoi(r.URL.Query().Get("since"))
		if since > len(l.lines) {
			since = len(l.lines)
		}
		json.NewEncoder(w).Encode(protocol.FetchResponse{
			Lines: l.lines[since:],
			Count: len(l.lines),
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

// followUntil trails the log until step reports it has seen enough, then stops
// the follow and returns everything printed.
func followUntil(t *testing.T, step func(printed string) bool) string {
	t.Helper()
	var buf syncBuffer
	// Only the follow goroutine touches the bufio.Writer.
	out := bufio.NewWriter(&buf)

	ctx, cancel := context.WithTimeout(context.Background(), followBudget)
	defer cancel()

	watching := make(chan struct{})
	go func() {
		defer close(watching)
		_ = followStream(ctx, out, "sometoken", false)
	}()

	require.Eventually(t, func() bool { return step(buf.String()) },
		followBudget, 5*time.Millisecond)

	cancel()
	<-watching
	return buf.String()
}

func TestFollowPrintsLinesAsTheyArrive(t *testing.T) {
	lockGlobalState(t)
	srv := &lineServer{found: true}
	pointClientAt(t, srv.start(t))
	setFollowInterval(t, 5*time.Millisecond)

	srv.append("early build output")
	queued := false
	got := followUntil(t, func(printed string) bool {
		if !strings.Contains(printed, "early build output") {
			return false
		}
		// Append only after the earlier line printed, proving it kept reading.
		if !queued {
			srv.append("printed later")
			queued = true
		}
		return strings.Contains(printed, "printed later")
	})

	require.Contains(t, got, "early build output")
	require.Contains(t, got, "printed later")
	require.Equal(t, 1, strings.Count(got, "early build output"),
		"a line already followed must not be reprinted on the next poll")
}

func TestFollowWaitsForAStreamThatDoesNotExistYet(t *testing.T) {
	lockGlobalState(t)
	srv := &lineServer{found: false}
	pointClientAt(t, srv.start(t))
	setFollowInterval(t, 5*time.Millisecond)

	// A watcher knows the token before the job streams, so an absent log waits.
	started := false
	got := followUntil(t, func(printed string) bool {
		if !started {
			srv.setFound(true)
			srv.append("build started at last")
			started = true
		}
		return strings.Contains(printed, "build started at last")
	})
	require.Contains(t, got, "build started at last")
}

func TestFollowRewindsWhenTheLogShrinks(t *testing.T) {
	lockGlobalState(t)
	srv := &lineServer{found: true}
	pointClientAt(t, srv.start(t))
	setFollowInterval(t, 5*time.Millisecond)

	srv.append("earlier attempt line a")
	srv.append("earlier attempt line b")
	srv.append("earlier attempt line c")

	rewound := false
	got := followUntil(t, func(printed string) bool {
		if !rewound {
			if !strings.Contains(printed, "earlier attempt line c") {
				return false
			}
			// Deleted and restarted shorter, leaving the cursor past the end.
			srv.restartWith("from the retry")
			rewound = true
			return false
		}
		return strings.Contains(printed, "from the retry")
	})
	require.Contains(t, got, "from the retry")
}

func setFollowInterval(t *testing.T, d time.Duration) {
	t.Helper()
	orig := fetchInterval
	fetchInterval = d
	t.Cleanup(func() { fetchInterval = orig })
}
