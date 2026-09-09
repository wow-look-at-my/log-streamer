package storage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

// groupExt keeps an index in a different file from a log.
const groupExt = ".grp"

// membership is an index line: a stream saying it belongs to a group. A stream
// registers per connection, and Members folds the repeats.
type membership struct {
	Token string    `json:"token"`
	Label string    `json:"label,omitempty"`
	At    time.Time `json:"at"`
}

func (s *Store) groupPath(group string) (string, error) {
	if !token.Validate(group) {
		return "", ErrBadToken
	}
	return filepath.Join(s.dir, group+groupExt), nil
}

// Join records that a stream belongs to a group. A failure to record is the
// caller's to report: the stream itself is unaffected, and losing the entry
// only hides that stream from a listing.
func (s *Store) Join(group, tok, label string) error {
	path, err := s.groupPath(group)
	if err != nil {
		return err
	}
	if !token.Validate(tok) {
		return ErrBadToken
	}

	line, err := json.Marshal(membership{Token: tok, Label: label, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	line = append(line, '\n')

	if s.maxTotalBytes > 0 && s.TotalBytes()+int64(len(line)) > s.maxTotalBytes {
		return ErrDiskFull
	}

	mu := s.shardFor(group)
	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return err
	}
	s.addTotalBytes(int64(len(line)))
	return nil
}

// Members lists a group's streams in registration order. Each carries the size
// of its own log, so a listing shows which legs produced output.
func (s *Store) Members(group string) ([]protocol.GroupMember, error) {
	path, err := s.groupPath(group)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	byToken := map[string]*protocol.GroupMember{}
	var order []string

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), maxRecordBytes)
	for sc.Scan() {
		var m membership
		if json.Unmarshal(sc.Bytes(), &m) != nil || !token.Validate(m.Token) {
			continue // a torn or foreign line must not lose the rest of the index
		}
		existing := byToken[m.Token]
		if existing == nil {
			byToken[m.Token] = &protocol.GroupMember{
				Token:     m.Token,
				Label:     m.Label,
				FirstSeen: m.At,
				LastSeen:  m.At,
			}
			order = append(order, m.Token)
			continue
		}
		if m.At.After(existing.LastSeen) {
			existing.LastSeen = m.At
		}
		if existing.Label == "" {
			existing.Label = m.Label
		}
	}

	out := make([]protocol.GroupMember, 0, len(order))
	for _, tok := range order {
		m := byToken[tok]
		m.Bytes = s.streamBytes(tok)
		out = append(out, *m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].FirstSeen.Before(out[j].FirstSeen) })
	return out, nil
}

// deleteGroup removes an index. The streams it named are separate files and
// expire on their own.
func (s *Store) deleteGroup(group string) error {
	path, err := s.groupPath(group)
	if err != nil {
		return err
	}
	mu := s.shardFor(group)
	mu.Lock()
	defer mu.Unlock()

	info, statErr := os.Stat(path)
	if err := os.Remove(path); err != nil {
		return err
	}
	if statErr == nil {
		s.addTotalBytes(-info.Size())
	}
	return nil
}

// streamBytes reports a member's log size, empty for a stream that wrote none.
func (s *Store) streamBytes(tok string) int64 {
	path, err := s.filePath(tok)
	if err != nil {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
