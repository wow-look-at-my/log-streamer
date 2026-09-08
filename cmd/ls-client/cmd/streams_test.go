package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

// clearDerivationEnv leaves nothing behind that could name a stream.
func clearDerivationEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"LOG_STREAMER_TOKEN", "LOG_STREAMER_GROUP", "LOG_STREAMER_STREAM_KEY",
		"LOG_STREAMER_NAME", "LOG_STREAMER_LABEL",
		"GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "GITHUB_ACTION",
	} {
		t.Setenv(name, "")
	}
	setDeriveFlags(t, "", "", "")
}

// A key is enough: the writer names its own stream, with nothing to hand over.
func TestAKeyAloneNamesTheStreamAndItsGroup(t *testing.T) {
	lockGlobalState(t)
	clearDerivationEnv(t)

	t.Setenv("LOG_STREAMER_STREAM_KEY", "shared-key")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_JOB", "test")

	tok, group := getStreamToken(), getStreamGroup()
	require.True(t, token.Validate(tok))
	require.True(t, token.Validate(group))
	require.NotEqual(t, tok, group, "a group is namespaced away from the streams it indexes")

	// A watcher computes both from the same facts, without the writer's help.
	watcherToken, err := token.Derive("shared-key", "owner/repo/12345/1/test")
	require.NoError(t, err)
	require.Equal(t, watcherToken, tok)

	watcherGroup, err := token.DeriveGroup("shared-key", "owner/repo/12345/1")
	require.NoError(t, err)
	require.Equal(t, watcherGroup, group)
}

// Matrix legs share every GITHUB_* value, so only the name separates them, and
// the group they land in must stay the same.
func TestALegSplitsTheStreamButNotTheGroup(t *testing.T) {
	lockGlobalState(t)
	clearDerivationEnv(t)

	t.Setenv("LOG_STREAMER_STREAM_KEY", "shared-key")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_JOB", "test")

	t.Setenv("LOG_STREAMER_NAME", "ubuntu-latest")
	linux, linuxGroup := getStreamToken(), getStreamGroup()

	t.Setenv("LOG_STREAMER_NAME", "macos-14")
	mac, macGroup := getStreamToken(), getStreamGroup()

	require.NotEqual(t, linux, mac, "each leg writes into its own stream")
	require.Equal(t, linuxGroup, macGroup, "both legs list under the run's group")
}

func TestNoKeyLeavesTheStreamUnnamed(t *testing.T) {
	lockGlobalState(t)
	clearDerivationEnv(t)

	require.Empty(t, getStreamToken())
	require.Empty(t, getStreamGroup())

	_, err := tokenArgOrDerived(nil)
	require.ErrorContains(t, err, "--key")
	_, err = groupArgOrDerived(nil)
	require.ErrorContains(t, err, "--key")
}

// The stream URL carries what the server needs to index the stream.
func TestStreamURLCarriesTheGroupAndLabel(t *testing.T) {
	lockGlobalState(t)
	clearDerivationEnv(t)
	orig := serverURL
	defer func() { serverURL = orig }()
	serverURL = "wss://logs.example.com"

	t.Setenv("LOG_STREAMER_STREAM_KEY", "shared-key")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_JOB", "test")

	got, err := streamURL()
	require.NoError(t, err)
	require.Contains(t, got, "token="+getStreamToken())
	require.Contains(t, got, "group="+getStreamGroup())
	require.Contains(t, got, "label=test", "the job names itself in the listing")
}

func TestStreamURLRefusesAMalformedGroup(t *testing.T) {
	lockGlobalState(t)
	clearDerivationEnv(t)

	tok, err := token.Generate()
	require.NoError(t, err)
	t.Setenv("LOG_STREAMER_TOKEN", tok)
	t.Setenv("LOG_STREAMER_GROUP", "nope")

	_, err = streamURL()
	require.ErrorContains(t, err, "group must be 64 hex characters")
}

func TestHumanBytes(t *testing.T) {
	require.Equal(t, "0B", humanBytes(0))
	require.Equal(t, "512B", humanBytes(512))
	require.Equal(t, "1.0KB", humanBytes(1024))
	require.Equal(t, "1.5MB", humanBytes(1024*1024*3/2))
	require.Equal(t, "2.0GB", humanBytes(2*1024*1024*1024))
}

// A group listing is what a watcher reads before it knows any leg's token.
func TestStreamsListsTheLegsOfARun(t *testing.T) {
	lockGlobalState(t)
	captureStdout(t)
	clearDerivationEnv(t)

	ts := startClientTestServer(t)
	pointClientAt(t, ts)

	t.Setenv("LOG_STREAMER_STREAM_KEY", "shared-key")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_JOB", "test")

	group := getStreamGroup()
	for _, leg := range []string{"ubuntu-latest", "macos-14"} {
		t.Setenv("LOG_STREAMER_NAME", leg)
		t.Setenv("LOG_STREAMER_LABEL", "test ("+leg+")")
		rootCmd.SetArgs([]string{"shell", writeScript(t, "echo built on "+leg+"\n")})
		require.NoError(t, rootCmd.Execute())
	}

	resp, err := fetchGroup(group)
	require.NoError(t, err)
	require.Equal(t, group, resp.Group)
	require.Len(t, resp.Streams, 2)

	labels := []string{resp.Streams[0].Label, resp.Streams[1].Label}
	require.ElementsMatch(t, []string{"test (ubuntu-latest)", "test (macos-14)"}, labels)

	for _, m := range resp.Streams {
		require.True(t, token.Validate(m.Token))
		require.Positive(t, m.Bytes, "a listed leg reports the size of its own log")

		// A listed token reads its own leg back.
		fetched, err := fetchSince(m.Token, 0)
		require.NoError(t, err)

		leg := strings.TrimSuffix(strings.TrimPrefix(m.Label, "test ("), ")")
		var text []string
		for _, line := range fetched.Lines {
			text = append(text, line.Line)
		}
		require.Contains(t, text, "built on "+leg)
	}
}
