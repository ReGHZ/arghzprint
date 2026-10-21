package app

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/internal/printer"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

type finished struct {
	status protocol.JobStatus
	errMsg string
	j      *job.Job
}

// tcpHooks prints for real over TCP — only the address table is test-controlled
type tcpHooks struct {
	mu       sync.Mutex
	addrs    map[string]string
	attempts map[string]int
	finished map[string]finished
	done     chan string
}

func newTCPHooks(addrs map[string]string) *tcpHooks {
	return &tcpHooks{addrs: addrs, attempts: map[string]int{}, finished: map[string]finished{}, done: make(chan string, 16)}
}

func (h *tcpHooks) setAddr(name, addr string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addrs[name] = addr
}

func (h *tcpHooks) printJob(j *job.Job, name string) error {
	if j.Type == "BROKEN" {
		return permanent(errors.New("parse template: unexpected }}"))
	}
	h.mu.Lock()
	addr := h.addrs[name]
	h.mu.Unlock()
	return printer.SendTCP(addr, []byte("job:"+j.ID))
}

func (h *tcpHooks) started(context.Context, *job.Job) {}

func (h *tcpHooks) attempted(j *job.Job) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attempts[j.ID]++
}

func (h *tcpHooks) finish(_ context.Context, j *job.Job, status protocol.JobStatus, errMsg, _ string) {
	h.mu.Lock()
	h.finished[j.ID] = finished{status: status, errMsg: errMsg, j: j}
	h.mu.Unlock()
	h.done <- j.ID
}

func (h *tcpHooks) result(id string) (finished, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.finished[id]
	return f, ok
}

func (h *tcpHooks) waitDone(t *testing.T, id string) finished {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-h.done:
			if got == id {
				f, _ := h.result(id)
				return f
			}
		case <-deadline:
			t.Fatalf("job %s never finished", id)
		}
	}
}

func livePrinter(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			io.Copy(io.Discard, conn)
			conn.Close()
		}
	}()
	return ln.Addr().String()
}

func deadPrinter(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

var fastPolicy = retryPolicy{base: 20 * time.Millisecond, max: 40 * time.Millisecond, maxAge: time.Hour}

func startLane(t *testing.T, ctx context.Context, name string, h laneHooks, p retryPolicy) *lane {
	t.Helper()
	l := newLane(name, h, p)
	go l.run(ctx)
	return l
}

func TestLaneDeadPrinterDoesNotBlockOther(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newTCPHooks(map[string]string{"Dapur": deadPrinter(t), "Bar": livePrinter(t)})
	kitchen := startLane(t, ctx, "Dapur", h, fastPolicy)
	bar := startLane(t, ctx, "Bar", h, fastPolicy)

	kitchen.queue.Push(&job.Job{ID: "k1", Type: "KITCHEN", ReceivedAt: time.Now()})
	bar.queue.Push(&job.Job{ID: "b1", Type: "BAR", ReceivedAt: time.Now()})

	if f := h.waitDone(t, "b1"); f.status != protocol.JobStatusCompleted {
		t.Fatalf("bar job = %+v", f)
	}
	if _, done := h.result("k1"); done {
		t.Fatal("kitchen job finished while its printer is dead")
	}
	time.Sleep(60 * time.Millisecond)
	if kitchen.online.Load() {
		t.Error("kitchen lane still reports online")
	}
	if kitchen.pending() != 1 {
		t.Errorf("kitchen pending = %d, want 1", kitchen.pending())
	}
}

func TestLaneRecoversWhenPrinterComesBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newTCPHooks(map[string]string{"Dapur": deadPrinter(t)})
	l := startLane(t, ctx, "Dapur", h, fastPolicy)
	l.queue.Push(&job.Job{ID: "k1", Type: "KITCHEN", ReceivedAt: time.Now()})

	time.Sleep(100 * time.Millisecond)
	h.setAddr("Dapur", livePrinter(t))

	f := h.waitDone(t, "k1")
	if f.status != protocol.JobStatusCompleted {
		t.Fatalf("job = %+v", f)
	}
	if f.j.Attempts < 1 {
		t.Errorf("attempts = %d, want ≥1 failed attempt before success", f.j.Attempts)
	}
	if !l.online.Load() {
		t.Error("lane not back online")
	}
}

func TestLaneFailsAfterMaxAge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newTCPHooks(map[string]string{"Dapur": deadPrinter(t)})
	l := startLane(t, ctx, "Dapur", h, fastPolicy)
	l.queue.Push(&job.Job{ID: "old", Type: "KITCHEN", ReceivedAt: time.Now().Add(-2 * time.Hour)})

	f := h.waitDone(t, "old")
	if f.status != protocol.JobStatusFailed || !strings.Contains(f.errMsg, "dial printer") {
		t.Fatalf("job = %+v", f)
	}
}

func TestLanePermanentErrorFailsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newTCPHooks(map[string]string{"Dapur": livePrinter(t)})
	l := startLane(t, ctx, "Dapur", h, fastPolicy)
	l.queue.Push(&job.Job{ID: "bad", Type: "BROKEN", ReceivedAt: time.Now()})

	f := h.waitDone(t, "bad")
	if f.status != protocol.JobStatusFailed || f.j.Attempts != 1 {
		t.Fatalf("job = %+v attempts=%d", f, f.j.Attempts)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attempts["bad"] != 0 {
		t.Error("permanent failure was recorded as a retryable attempt")
	}
	if !l.online.Load() {
		t.Error("a template error must not mark the printer offline")
	}
}

func TestLaneStopsOnCancelWithoutFinishing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	h := newTCPHooks(map[string]string{"Dapur": deadPrinter(t)})
	l := newLane("Dapur", h, retryPolicy{base: time.Hour, max: time.Hour, maxAge: time.Hour})
	l.queue.Push(&job.Job{ID: "k1", Type: "KITCHEN", ReceivedAt: time.Now()})

	stopped := make(chan struct{})
	go func() { l.run(ctx); close(stopped) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("lane did not stop on cancel")
	}
	if _, done := h.result("k1"); done {
		t.Error("job finished on shutdown — it should stay unfinished for resume")
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		cur, max, want time.Duration
	}{
		{5 * time.Second, time.Minute, 10 * time.Second},
		{40 * time.Second, time.Minute, time.Minute},
		{time.Minute, time.Minute, time.Minute},
	}
	for _, tc := range cases {
		if got := nextBackoff(tc.cur, tc.max); got != tc.want {
			t.Errorf("nextBackoff(%v, %v) = %v, want %v", tc.cur, tc.max, got, tc.want)
		}
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	p := defaultRetryPolicy(30)
	if p.base != 5*time.Second || p.max != 60*time.Second || p.maxAge != 30*time.Minute {
		t.Errorf("policy = %+v", p)
	}
}
