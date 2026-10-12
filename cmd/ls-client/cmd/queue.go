package cmd

import "sync"

// queue holds frames the sender has not yet put on the wire.
type queue struct {
	mu     sync.Mutex
	wake   *sync.Cond
	frames [][]byte
	closed bool
}

func newQueue() *queue {
	q := &queue{}
	q.wake = sync.NewCond(&q.mu)
	return q
}

// Push adds a frame and returns.
func (q *queue) Push(frame []byte) {
	q.mu.Lock()
	q.frames = append(q.frames, frame)
	q.wake.Broadcast()
	q.mu.Unlock()
}

// Close says no more frames are coming. A sender keeps draining what is left,
// which is what makes the tail safe.
func (q *queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.wake.Broadcast()
	q.mu.Unlock()
}

// Head blocks until a frame is waiting and returns it without removing it. A
// retry after a dropped connection therefore sends the same frame again. ok is
// false when the queue is closed and empty.
func (q *queue) Head() (frame []byte, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.frames) == 0 && !q.closed {
		q.wake.Wait()
	}
	if len(q.frames) == 0 {
		return nil, false
	}
	return q.frames[0], true
}

// Pop drops the head, which the sender does after the server holds it.
func (q *queue) Pop() {
	q.mu.Lock()
	if len(q.frames) > 0 {
		q.frames = q.frames[1:]
	}
	q.mu.Unlock()
}
