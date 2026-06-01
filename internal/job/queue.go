package job

import (
	"container/heap"
	"sync"
)

// Queue is a thread-safe priority queue for print jobs.
// Higher Priority value = processed first. FIFO within the same priority.
type Queue struct {
	mu   sync.Mutex
	heap jobHeap
	seq  int64 // tiebreaker to preserve FIFO within same priority
}

func NewQueue() *Queue {
	q := &Queue{}
	heap.Init(&q.heap)
	return q
}

func (q *Queue) Push(j *Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.seq++
	heap.Push(&q.heap, &entry{job: j, seq: q.seq})
}

// Pop removes and returns the highest-priority job.
// Returns nil if the queue is empty.
func (q *Queue) Pop() *Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.heap.Len() == 0 {
		return nil
	}
	return heap.Pop(&q.heap).(*entry).job
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.heap.Len()
}

// entry wraps a Job with sequencing for stable ordering within the same priority.
type entry struct {
	job *Job
	seq int64
}

type jobHeap []*entry

func (h jobHeap) Len() int { return len(h) }

func (h jobHeap) Less(i, j int) bool {
	if h[i].job.Priority != h[j].job.Priority {
		// higher priority value → comes first
		return h[i].job.Priority > h[j].job.Priority
	}
	// same priority → earlier arrival first (FIFO)
	return h[i].seq < h[j].seq
}

func (h jobHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *jobHeap) Push(x any) {
	*h = append(*h, x.(*entry))
}

func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}
