package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

type recordSink struct {
	mu   sync.Mutex
	jobs []*job.Job
}

func (s *recordSink) Accept(_ context.Context, j *job.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
}

func newTestAgent(t *testing.T, backendURL string, sink Sink) *Agent {
	t.Helper()
	m, err := config.NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := m.Get()
	c.BackendURL = backendURL
	c.EnabledTypes = map[string]bool{"KITCHEN": true}
	c.PriorityMap = map[string]int{"KITCHEN": 2}
	if err := m.Save(c); err != nil {
		t.Fatal(err)
	}
	return New(m, sink, nil)
}

func TestUpdateStatusClassifiesResponses(t *testing.T) {
	cases := []struct {
		name         string
		code         int
		wantErr      bool
		wantRejected bool
	}{
		{"204 ok", http.StatusNoContent, false, false},
		{"200 ok", http.StatusOK, false, false},
		{"409 rejected", http.StatusConflict, true, true},
		{"404 rejected", http.StatusNotFound, true, true},
		{"400 rejected", http.StatusBadRequest, true, true},
		{"500 retryable", http.StatusInternalServerError, true, false},
		// a wrong/rotated token must not throw finals away — they retry until it's fixed
		{"401 retryable", http.StatusUnauthorized, true, false},
		{"403 retryable", http.StatusForbidden, true, false},
		{"429 retryable", http.StatusTooManyRequests, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
			}))
			defer srv.Close()

			err := newTestAgent(t, srv.URL, &recordSink{}).UpdateStatus(context.Background(), "j1", protocol.JobStatusCompleted, "")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if errors.Is(err, ErrRejected) != tc.wantRejected {
				t.Fatalf("rejected = %v, want %v (err %v)", errors.Is(err, ErrRejected), tc.wantRejected, err)
			}
		})
	}
}

func TestUpdateStatusNetworkErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	err := newTestAgent(t, url, &recordSink{}).UpdateStatus(context.Background(), "j1", protocol.JobStatusCompleted, "")
	if err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want non-rejected network error", err)
	}
}

func TestClaimAndAccept(t *testing.T) {
	cases := []struct {
		name       string
		jobType    string
		claimed    bool
		wantAccept bool
		wantClaim  bool
	}{
		{"claimed", "KITCHEN", true, true, true},
		{"lost claim", "KITCHEN", false, false, true},
		{"type disabled", "BAR", true, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var claimHit bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/claim") {
					claimHit = true
					w.Header().Set("Content-Type", "application/json")
					if tc.claimed {
						w.Write([]byte(`{"claimed":true}`))
					} else {
						w.Write([]byte(`{"claimed":false}`))
					}
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			sink := &recordSink{}
			a := newTestAgent(t, srv.URL, sink)
			got := a.claimAndAccept(context.Background(), protocol.JobEnvelope{JobID: "j1", Type: tc.jobType, Payload: map[string]any{}})

			if got != tc.wantAccept || claimHit != tc.wantClaim {
				t.Fatalf("accepted=%v claimHit=%v", got, claimHit)
			}
			if tc.wantAccept {
				if len(sink.jobs) != 1 || sink.jobs[0].Priority != 2 {
					t.Fatalf("sink = %+v", sink.jobs)
				}
			} else if len(sink.jobs) != 0 {
				t.Fatalf("sink got %d jobs, want 0", len(sink.jobs))
			}
		})
	}
}

func TestBackendCallsTimeOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	if httpClient.Timeout <= 0 || httpClient.Timeout > 15*time.Second {
		t.Fatalf("default backend timeout = %v, want a bounded timeout", httpClient.Timeout)
	}
	old := httpClient.Timeout
	httpClient.Timeout = 50 * time.Millisecond
	defer func() { httpClient.Timeout = old }()

	start := time.Now()
	err := newTestAgent(t, srv.URL, &recordSink{}).UpdateStatus(context.Background(), "j1", protocol.JobStatusCompleted, "")
	if err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want a retryable timeout", err)
	}
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Errorf("call took %v — no client timeout", took)
	}
}
