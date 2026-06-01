package storage

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

// Sentinel errors returned by Writer.Append when a configured limit would be
// exceeded. The caller should stop the stream when it sees one of these.
var (
	ErrStreamFull = errors.New("stream byte limit exceeded")
	ErrDiskFull   = errors.New("total storage limit exceeded")
	ErrBadToken   = errors.New("invalid token")
)

const (
	numShards = 256
	fileExt   = ".bin"

	// maxRecordBytes bounds a single on-disk record when reading back. A frame
	// arrives in one WebSocket message (capped by the server's read limit), so
	// legitimate records stay well under this; the ceiling only guards Fetch
	// against a corrupt length prefix.
	maxRecordBytes = 8 << 20 // 8 MiB
)

// Options configures a Store.
type Options struct {
	Dir            string
	MaxStreamBytes int64         // per-stream cap in bytes; 0 = unlimited
	MaxTotalBytes  int64         // total on-disk cap in bytes; 0 = unlimited
	TTL            time.Duration // delete streams older than this; 0 = never
}

type Store struct {
	dir            string
	maxStreamBytes int64
	maxTotalBytes  int64
	ttl            time.Duration

	// shards serialize append-vs-delete on the same token without serializing
	// the whole server. A token maps to a shard by hash; distinct streams
	// almost never contend.
	shards [numShards]sync.Mutex

	// totalBytes is an approximate running sum of on-disk file sizes used to
	// enforce MaxTotalBytes. It is seeded from disk in New, errs high under
	// concurrent delete-while-writing (conservative), and is reset on restart.
	totalBytes int64
}

func New(opts Options) (*Store, error) {
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		dir:            opts.Dir,
		maxStreamBytes: opts.MaxStreamBytes,
		maxTotalBytes:  opts.MaxTotalBytes,
		ttl:            opts.TTL,
	}

	entries, err := os.ReadDir(opts.Dir)
	if err != nil {
		return nil, err
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	atomic.StoreInt64(&s.totalBytes, total)
	return s, nil
}

func (s *Store) shardFor(tok string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tok))
	return &s.shards[h.Sum32()%numShards]
}

func (s *Store) filePath(tok string) (string, error) {
	// Defense in depth: never derive a path from an unvalidated token, so a
	// token can never traverse out of the data directory.
	if !token.Validate(tok) {
		return "", ErrBadToken
	}
	return filepath.Join(s.dir, tok+fileExt), nil
}

// TotalBytes returns the approximate total on-disk usage.
func (s *Store) TotalBytes() int64 { return atomic.LoadInt64(&s.totalBytes) }

// Writer holds an open append handle for one stream's lifetime so the server
// does not pay an open/close syscall per frame.
type Writer struct {
	store       *Store
	tok         string
	f           *os.File
	streamBytes int64
}

// OpenWriter opens (creating if needed) the append handle for a stream.
func (s *Store) OpenWriter(tok string) (*Writer, error) {
	path, err := s.filePath(tok)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Writer{store: s, tok: tok, f: f}, nil
}

// Append stores one frame body as a length-prefixed record:
// [uvarint len(body)][body]. It enforces the per-stream and total-disk caps,
// returning ErrStreamFull / ErrDiskFull when a limit would be exceeded.
func (w *Writer) Append(body []byte) error {
	s := w.store

	bodyLen := int64(len(body))
	if s.maxStreamBytes > 0 && w.streamBytes+bodyLen > s.maxStreamBytes {
		return ErrStreamFull
	}

	var hdr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(hdr[:], uint64(len(body)))
	diskLen := int64(n) + bodyLen

	if s.maxTotalBytes > 0 && atomic.LoadInt64(&s.totalBytes)+diskLen > s.maxTotalBytes {
		return ErrDiskFull
	}

	rec := make([]byte, n+len(body))
	copy(rec, hdr[:n])
	copy(rec[n:], body)

	mu := s.shardFor(w.tok)
	mu.Lock()
	_, err := w.f.Write(rec)
	mu.Unlock()
	if err != nil {
		return err
	}

	w.streamBytes += bodyLen
	atomic.AddInt64(&s.totalBytes, diskLen)
	return nil
}

// Close releases the append handle.
func (w *Writer) Close() error { return w.f.Close() }

// Fetch reads a stream and reassembles its frames into whole lines. It splits
// each stream's concatenated payload on '\n', giving every line the timestamp
// of the frame that contained its first byte. It reads without a lock, so it
// reflects data up to the current end of file and skips a torn final record.
func (s *Store) Fetch(tok string) ([]protocol.StreamMessage, error) {
	path, err := s.filePath(tok)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	br := bufio.NewReader(f)

	type pending struct {
		ts      time.Time
		data    []byte
		started bool
	}
	open := map[protocol.StreamID]*pending{}
	var lines []protocol.StreamMessage

	emit := func(stream protocol.StreamID, p *pending) {
		lines = append(lines, protocol.StreamMessage{
			Timestamp: p.ts,
			Line:      string(p.data),
			Stream:    stream.String(),
		})
		p.data = p.data[:0]
		p.started = false
	}

	for {
		n, err := binary.ReadUvarint(br)
		if err != nil || n < protocol.FrameHeaderSize || n > maxRecordBytes {
			break // clean EOF, truncated prefix, or corrupt record: stop
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(br, body); err != nil {
			break // torn final record
		}
		stream, ts, payload, err := protocol.DecodeWire(body)
		if err != nil {
			continue
		}
		p := open[stream]
		if p == nil {
			p = &pending{}
			open[stream] = p
		}
		for len(payload) > 0 {
			if !p.started {
				p.ts = ts
				p.started = true
			}
			i := bytes.IndexByte(payload, '\n')
			if i < 0 {
				p.data = append(p.data, payload...)
				break
			}
			p.data = append(p.data, payload[:i]...)
			emit(stream, p)
			payload = payload[i+1:]
		}
	}

	// Flush streams left mid-line (no trailing newline, writer crash, or a
	// fetch that raced an in-flight write). Deterministic order.
	if len(open) > 0 {
		ids := make([]int, 0, len(open))
		for id, p := range open {
			if p.started || len(p.data) > 0 {
				ids = append(ids, int(id))
			}
		}
		sort.Ints(ids)
		for _, id := range ids {
			p := open[protocol.StreamID(id)]
			lines = append(lines, protocol.StreamMessage{
				Timestamp: p.ts,
				Line:      string(p.data),
				Stream:    protocol.StreamID(id).String(),
			})
		}
	}

	return lines, nil
}

func (s *Store) Delete(tok string) error {
	path, err := s.filePath(tok)
	if err != nil {
		return err
	}
	mu := s.shardFor(tok)
	mu.Lock()
	defer mu.Unlock()

	info, statErr := os.Stat(path)
	if err := os.Remove(path); err != nil {
		return err
	}
	if statErr == nil {
		atomic.AddInt64(&s.totalBytes, -info.Size())
	}
	return nil
}

func (s *Store) Exists(tok string) bool {
	path, err := s.filePath(tok)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Sweep deletes streams whose file modification time is older than the TTL. It
// is a no-op when TTL is 0. Returns the number of streams removed.
func (s *Store) Sweep(now time.Time) (int, error) {
	if s.ttl <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	removed := 0
	cutoff := now.Add(-s.ttl)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != fileExt {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		tok := strings.TrimSuffix(e.Name(), fileExt)
		if err := s.Delete(tok); err == nil {
			removed++
		}
	}
	return removed, nil
}
