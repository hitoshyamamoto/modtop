package transport

import (
	"sync"
	"time"
)

// FrameLogSize is how many frames the log keeps.
const FrameLogSize = 200

// Direction tells whether a frame was sent or received.
type Direction uint8

// Frame directions.
const (
	TX Direction = iota
	RX
)

func (d Direction) String() string {
	if d == TX {
		return "TX"
	}
	return "RX"
}

// Frame is one raw frame (or a receive event with no bytes, such as a
// timeout) with a short decoded note.
type Frame struct {
	At   time.Time
	Dir  Direction
	Raw  []byte
	Note string
}

// FrameLog is a fixed-size ring buffer of frames, safe for concurrent use.
type FrameLog struct {
	mu    sync.Mutex
	buf   [FrameLogSize]Frame
	next  int    // index of the next write
	total uint64 // frames ever added
}

// NewFrameLog returns an empty log.
func NewFrameLog() *FrameLog {
	return &FrameLog{}
}

// Add appends a frame, dropping the oldest when full. The raw bytes are copied.
func (l *FrameLog) Add(f Frame) {
	if l == nil {
		return
	}
	f.Raw = append([]byte(nil), f.Raw...)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf[l.next] = f
	l.next = (l.next + 1) % FrameLogSize
	l.total++
}

// Snapshot returns the frames currently held, oldest first, and the total
// number of frames ever added (useful to detect new arrivals).
func (l *FrameLog) Snapshot() ([]Frame, uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := int(min(l.total, FrameLogSize))
	out := make([]Frame, 0, n)
	start := (l.next - n + FrameLogSize) % FrameLogSize
	for i := 0; i < n; i++ {
		out = append(out, l.buf[(start+i)%FrameLogSize])
	}
	return out, l.total
}
