package storage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

type Store struct {
	dir string
	mu  sync.RWMutex
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Append(token string, msg protocol.StreamMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.filePath(token), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(msg)
}

func (s *Store) Fetch(token string) ([]protocol.StreamMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := os.Open(s.filePath(token))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []protocol.StreamMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var msg protocol.StreamMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		lines = append(lines, msg)
	}
	return lines, scanner.Err()
}

func (s *Store) Delete(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.filePath(token))
}

func (s *Store) Exists(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(s.filePath(token))
	return err == nil
}

func (s *Store) filePath(token string) string {
	return filepath.Join(s.dir, token+".jsonl")
}
