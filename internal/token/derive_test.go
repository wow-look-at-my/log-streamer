package token

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveIsDeterministic(t *testing.T) {
	a, err := Derive("key", "wow-look-at-my/log-streamer/42/1/test")
	require.NoError(t, err)
	b, err := Derive("key", "wow-look-at-my/log-streamer/42/1/test")
	require.NoError(t, err)

	// The whole scheme rests on both sides computing the same token offline.
	require.Equal(t, a, b)
	require.True(t, Validate(a), "a derived token must be a usable stream token")
}

func TestDeriveSeparatesContextsAndKeys(t *testing.T) {
	base, err := Derive("key", "repo/1/1/build")
	require.NoError(t, err)

	otherContext, err := Derive("key", "repo/2/1/build")
	require.NoError(t, err)
	require.NotEqual(t, base, otherContext, "a different run must get its own stream")

	otherKey, err := Derive("other-key", "repo/1/1/build")
	require.NoError(t, err)
	require.NotEqual(t, base, otherKey, "the key must actually gate the token")
}

func TestDeriveRefusesMissingInputs(t *testing.T) {
	_, err := Derive("", "repo/1/1/build")
	require.ErrorIs(t, err, ErrNoKey)

	_, err = Derive("key", "")
	require.ErrorIs(t, err, ErrNoContext)
}

func TestContextSkipsEmptyParts(t *testing.T) {
	require.Equal(t, "repo/7/build", Context("repo", "", "7", "  ", "build"))
	require.Equal(t, "", Context("", "   "))
}

func TestContextSeparatesNeighbouringParts(t *testing.T) {
	// Joining without a separator would let distinct builds collide.
	require.NotEqual(t, Context("ab", "c"), Context("a", "bc"))
}
