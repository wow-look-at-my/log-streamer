package cmd

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
)

// spool is the ordered, durable hand-off between the command's output and the
// network. Append writes and returns; it never waits on a socket, so a stalled
// stream cannot slow the command and a broken one loses nothing.
//
// Records are [uvarint len][frame], the shape the server stores, so a byte
// count the server reports names a position in here.
type spool struct {
	mu   sync.Mutex
	wake *sync.Cond
	f    *os.File
	path string
	// written is how many record bytes exist; read is how far the sender has got.
	written int64
	read    int64
	closed  bool
}

// newSpool opens the backing file. The file is unlinked at once: nothing else
// reads it, and an interrupted run must not leave one behind.
func newSpool(dir string) (*spool, error) {
	f, err := os.CreateTemp(dir, "ls-spool-*")
	if err != nil {
		return nil, err
	}
	s := &spool{f: f, path: f.Name()}
	s.wake = sync.NewCond(&s.mu)
	return s, nil
}

// Append stores a frame and returns. A disk error is real and is reported; a
// network that is down is not this call's concern.
func (s *spool) Append(frame []byte) error {
	var hdr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(hdr[:], uint64(len(frame)))
	rec := make([]byte, 0, n+len(frame))
	rec = append(rec, hdr[:n]...)
	rec = append(rec, frame...)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("spool is closed")
	}
	if _, err := s.f.WriteAt(rec, s.written); err != nil {
		return err
	}
	s.written += int64(len(rec))
	s.wake.Broadcast()
	return nil
}

// Close says no more frames are coming. A sender still draining keeps going
// until it reaches the end, which is what makes the tail safe.
func (s *spool) Close() {
	s.mu.Lock()
	s.closed = true
	s.wake.Broadcast()
	s.mu.Unlock()
}

// Remove releases the backing file once the sender is finished with it.
func (s *spool) Remove() {
	s.f.Close()
	os.Remove(s.path)
}

// next blocks until a frame is available at or after offset, and returns it
// with the offset that follows. ok is false only when the spool is closed and
// every frame has been handed out.
func (s *spool) next(offset int64) (frame []byte, nextOffset int64, ok bool) {
	s.mu.Lock()
	for offset >= s.written && !s.closed {
		s.wake.Wait()
	}
	written := s.written
	s.mu.Unlock()

	if offset >= written {
		return nil, offset, false
	}

	var hdr [binary.MaxVarintLen64]byte
	n, err := s.f.ReadAt(hdr[:], offset)
	if n == 0 && err != nil {
		return nil, offset, false
	}
	size, used := binary.Uvarint(hdr[:n])
	if used <= 0 {
		return nil, offset, false
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(s.f, offset+int64(used), int64(size)), buf); err != nil {
		return nil, offset, false
	}
	return buf, offset + int64(used) + int64(size), true
}

// pending reports how many record bytes the sender has not yet reached, which
// is what a drain waits to reach zero.
func (s *spool) pending() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written - s.read
}

// isClosed says whether any more frames can arrive.
func (s *spool) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// advance records how far the sender has got, so pending is answerable.
func (s *spool) advance(offset int64) {
	s.mu.Lock()
	s.read = offset
	s.mu.Unlock()
}
