package job

import (
	"sync"
	"time"
)

// Record is an immutable snapshot of a completed or failed job.
type Record struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	Printer     string    `json:"printer"`
	Attempts    int       `json:"attempts"`
	Error       string    `json:"error,omitempty"`
	ReceivedAt  time.Time `json:"receivedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// History is a thread-safe ring buffer of the last N job records.
type History struct {
	mu  sync.Mutex
	buf []Record
	max int
}

func NewHistory(max int) *History {
	return &History{max: max, buf: make([]Record, 0, max)}
}

// Add prepends a record and trims to max capacity.
func (h *History) Add(r Record) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf = append([]Record{r}, h.buf...)
	if len(h.buf) > h.max {
		h.buf = h.buf[:h.max]
	}
}

// All returns a copy of all records, newest first.
func (h *History) All() []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Record, len(h.buf))
	copy(out, h.buf)
	return out
}
