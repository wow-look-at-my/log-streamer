package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func groupStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(Options{Dir: t.TempDir()})
	require.NoError(t, err)
	return s
}

// hexToken builds a token of the shape the store accepts.
func hexToken(t *testing.T, fill byte) string {
	t.Helper()
	return strings.Repeat(string(fill), 64)
}

func TestGroupListsItsStreams(t *testing.T) {
	s := groupStore(t)
	group := hexToken(t, 'a')
	first, second := hexToken(t, 'b'), hexToken(t, 'c')

	require.NoError(t, s.Join(group, first, "test (ubuntu-latest)"))
	require.NoError(t, s.Join(group, second, "test (macos-14)"))

	// A stream that wrote something reports its size to the listing.
	w, err := s.OpenWriter(first)
	require.NoError(t, err)
	require.NoError(t, w.Append(make([]byte, 32)))
	require.NoError(t, w.Close())

	members, err := s.Members(group)
	require.NoError(t, err)
	require.Len(t, members, 2)

	require.Equal(t, first, members[0].Token)
	require.Equal(t, "test (ubuntu-latest)", members[0].Label)
	require.Positive(t, members[0].Bytes)

	require.Equal(t, second, members[1].Token)
	require.Zero(t, members[1].Bytes, "a leg that opened and wrote nothing still lists")
}

// Every step of a job opens its own connection, so a leg registers repeatedly.
func TestGroupFoldsRepeatedRegistrations(t *testing.T) {
	s := groupStore(t)
	group, tok := hexToken(t, 'a'), hexToken(t, 'b')

	require.NoError(t, s.Join(group, tok, "test"))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, s.Join(group, tok, "test"))

	members, err := s.Members(group)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.True(t, members[0].LastSeen.After(members[0].FirstSeen),
		"the latest registration is what says a leg is still running")
}

func TestGroupRefusesATokenItCannotStore(t *testing.T) {
	s := groupStore(t)
	good := hexToken(t, 'a')

	require.ErrorIs(t, s.Join("../escape", good, ""), ErrBadToken)
	require.ErrorIs(t, s.Join(good, "not-a-token", ""), ErrBadToken)

	_, err := s.Members("../escape")
	require.ErrorIs(t, err, ErrBadToken)
}

func TestGroupReportsAnUnknownGroupAsMissing(t *testing.T) {
	s := groupStore(t)
	_, err := s.Members(hexToken(t, 'd'))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// A torn write must cost the reader that line, never the rest of the index.
func TestGroupSkipsAnUnreadableLine(t *testing.T) {
	s := groupStore(t)
	group, tok := hexToken(t, 'a'), hexToken(t, 'b')
	require.NoError(t, s.Join(group, tok, "test"))

	path := filepath.Join(s.dir, group+groupExt)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("{\"token\":\"short\"}\n{not json\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	members, err := s.Members(group)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, tok, members[0].Token)
}

// An index outlives nothing: it expires with the logs it points at.
func TestSweepRemovesAnExpiredGroup(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)

	group, tok := hexToken(t, 'a'), hexToken(t, 'b')
	require.NoError(t, s.Join(group, tok, "test"))

	path := filepath.Join(dir, group+groupExt)
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	removed, err := s.Sweep(time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.NoFileExists(t, path)
}

func TestGroupCountsAgainstTheTotalCap(t *testing.T) {
	s, err := New(Options{Dir: t.TempDir(), MaxTotalBytes: 32})
	require.NoError(t, err)

	require.ErrorIs(t, s.Join(hexToken(t, 'a'), hexToken(t, 'b'), "a label longer than the cap allows"),
		ErrDiskFull)
}
