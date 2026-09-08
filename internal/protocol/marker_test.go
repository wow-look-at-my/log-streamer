package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkerRoundTrip(t *testing.T) {
	code := 3
	in := Marker{
		Event: EventStepEnd,
		Step:  "__run_2",
		Job:   "test",
		Cmd:   "make build",
		Exit:  &code,
	}

	encoded, err := EncodeMarker(in)
	require.NoError(t, err)
	require.Equal(t, byte('\n'), encoded[len(encoded)-1],
		"a marker occupies a line, so the store reassembles it like any other")

	// The store strips the newline when it splits lines, so parse without it.
	got, ok := ParseMarker(string(encoded[:len(encoded)-1]))
	require.True(t, ok)
	require.Equal(t, in.Step, got.Step)
	require.Equal(t, in.Cmd, got.Cmd)
	require.NotNil(t, got.Exit)
	require.Equal(t, code, *got.Exit)
}

// A reader must not mistake ordinary output for structure.
func TestParseMarkerRejectsNonMarkers(t *testing.T) {
	for _, line := range []string{
		"",
		"make: *** [build] Error 1",
		`{"event":"something_else"}`,
		`{"event":`,
		`{"not":"an event"}`,
	} {
		_, ok := ParseMarker(line)
		require.False(t, ok, "line %q", line)
	}
}

// The server renders the stream name, and a deployed server can be older than
// the client fetching from it.
func TestIsMarkerStreamAcceptsTheOlderSpelling(t *testing.T) {
	require.True(t, IsMarkerStream("marker"))
	require.True(t, IsMarkerStream("stream3"), "a server predating markers names the wire id")
	require.False(t, IsMarkerStream("stdout"))
	require.False(t, IsMarkerStream("stream4"))
}

func TestMarkerLabelPrefersTheNameItsAuthorGave(t *testing.T) {
	require.Equal(t, "Build", Marker{Name: "Build", Cmd: "make", Step: "__run"}.Label())
	require.Equal(t, "make", Marker{Cmd: "make", Step: "__run"}.Label())
	require.Equal(t, "__run", Marker{Step: "__run"}.Label())
	require.Equal(t, "(unnamed)", Marker{}.Label())
}
