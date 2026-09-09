package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

const testStreamToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setStreamToken(t *testing.T, tok string) {
	t.Helper()
	orig := streamToken
	streamToken = tok
	t.Cleanup(func() { streamToken = orig })
}

func TestStreamURLNamesTheCallersStream(t *testing.T) {
	lockGlobalState(t)
	serverURL = "ws://logs.example.com"
	t.Cleanup(func() { serverURL = "" })

	setStreamToken(t, "")
	got, err := streamURL()
	require.NoError(t, err)
	require.Equal(t, "ws://logs.example.com/api/stream", got)

	setStreamToken(t, testStreamToken)
	got, err = streamURL()
	require.NoError(t, err)
	require.Equal(t, "ws://logs.example.com/api/stream?token="+testStreamToken, got)
}

func TestStreamURLReadsTheTokenFromTheEnvironment(t *testing.T) {
	lockGlobalState(t)
	serverURL = "ws://logs.example.com"
	t.Cleanup(func() { serverURL = "" })
	setStreamToken(t, "")

	// CI passes the token by environment far more often than by flag.
	t.Setenv("LOG_STREAMER_TOKEN", testStreamToken)
	got, err := streamURL()
	require.NoError(t, err)
	require.Contains(t, got, "token="+testStreamToken)
}

func TestStreamURLRejectsAMalformedToken(t *testing.T) {
	lockGlobalState(t)
	setStreamToken(t, "../../etc/passwd")

	// Refuse locally rather than let the server close the socket mid-build.
	_, err := streamURL()
	require.Error(t, err)
	require.Contains(t, err.Error(), "64 hex characters")
}

func TestTokenGenerateProducesAUsableToken(t *testing.T) {
	lockGlobalState(t)
	captureStdout(t)
	require.NoError(t, runTokenGenerate(tokenGenerateCmd, nil))
}

func TestTokenDeriveMatchesTheLibrary(t *testing.T) {
	lockGlobalState(t)
	captureStdout(t)

	setDeriveFlags(t, "shared-key", "", "")
	t.Setenv("GITHUB_REPOSITORY", "wow-look-at-my/log-streamer")
	t.Setenv("GITHUB_RUN_ID", "42")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_JOB", "test")

	require.NoError(t, runTokenDerive(tokenDeriveCmd, nil))

	// The context the command builds must match what a reader reproduces.
	want, err := token.Derive("shared-key", "wow-look-at-my/log-streamer/42/1/test")
	require.NoError(t, err)
	require.Equal(t, want, mustDerive(t))
}

func TestTokenDeriveSeparatesMatrixLegsByName(t *testing.T) {
	lockGlobalState(t)
	t.Setenv("GITHUB_REPOSITORY", "wow-look-at-my/log-streamer")
	t.Setenv("GITHUB_RUN_ID", "42")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_JOB", "test")

	// Matrix legs share GITHUB_JOB, so without --name they share a stream.
	setDeriveFlags(t, "k", "", "linux")
	linux := mustDerive(t)
	setDeriveFlags(t, "k", "", "windows")
	require.NotEqual(t, linux, mustDerive(t))
}

func TestTokenDeriveRefusesWithoutAKey(t *testing.T) {
	lockGlobalState(t)
	setDeriveFlags(t, "", "some/context", "")
	t.Setenv("LOG_STREAMER_STREAM_KEY", "")

	// A keyless token is reproducible by anyone reading the run's metadata.
	err := runTokenDerive(tokenDeriveCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LOG_STREAMER_STREAM_KEY")
}

func TestTokenDeriveRefusesWithoutAContext(t *testing.T) {
	lockGlobalState(t)
	setDeriveFlags(t, "k", "", "")
	for _, k := range []string{"GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB"} {
		t.Setenv(k, "")
	}

	err := runTokenDerive(tokenDeriveCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--context")
}

func setDeriveFlags(t *testing.T, key, context, name string) {
	t.Helper()
	origKey, origContext, origName := deriveKey, deriveContext, deriveName
	deriveKey, deriveContext, deriveName = key, context, name
	t.Cleanup(func() {
		deriveKey, deriveContext, deriveName = origKey, origContext, origName
	})
}

func mustDerive(t *testing.T) string {
	t.Helper()
	key := deriveKey
	tok, err := token.Derive(key, derivedContext())
	require.NoError(t, err)
	require.True(t, token.Validate(tok))
	require.False(t, strings.Contains(tok, "/"))
	return tok
}
