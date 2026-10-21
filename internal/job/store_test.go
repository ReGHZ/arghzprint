package job

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newJob(id string, received time.Time) *Job {
	return &Job{
		ID:       id,
		Type:     "KITCHEN",
		Priority: 2,
		Payload: map[string]any{
			"orderId": float64(42),
			"items":   []any{map[string]any{"name": "Nasi Goreng"}},
		},
		ReceivedAt: received,
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertDedupe(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "t.db"))
	j := newJob("job-1", time.Now())

	first, err := s.Insert(j)
	if err != nil || !first {
		t.Fatalf("first insert = %v, %v", first, err)
	}
	second, err := s.Insert(j)
	if err != nil || second {
		t.Fatalf("second insert = %v, %v — want false", second, err)
	}
}

func TestLookup(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "t.db"))

	if found, _, err := s.Lookup("nope"); err != nil || found {
		t.Fatalf("missing lookup = %v, %v", found, err)
	}

	s.Insert(newJob("job-1", time.Now()))
	found, final, err := s.Lookup("job-1")
	if err != nil || !found || final != nil {
		t.Fatalf("unfinished lookup = %v, %v, %v", found, final, err)
	}

	if err := s.SetFinal("job-1", "COMPLETED", "", "Dapur", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	found, final, err = s.Lookup("job-1")
	if err != nil || !found || final == nil || final.Status != "COMPLETED" {
		t.Fatalf("finished lookup = %v, %+v, %v", found, final, err)
	}
}

func TestUnfinishedSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	t0 := time.UnixMilli(time.Now().UnixMilli())

	s1, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Insert(newJob("job-1", t0))
	s1.Insert(newJob("job-2", t0.Add(time.Second)))
	s1.RecordAttempt("job-1", 2, "dial refused")
	s1.SetFinal("job-2", "COMPLETED", "", "Dapur", 1, time.Now())
	s1.Close()

	s2 := openTestStore(t, path)
	jobs, err := s2.Unfinished()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("unfinished = %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.ID != "job-1" || j.Attempts != 2 || j.LastErr != "dial refused" || j.Priority != 2 {
		t.Errorf("job = %+v", j)
	}
	if j.Payload["orderId"] != float64(42) {
		t.Errorf("payload = %v", j.Payload)
	}
	if !j.ReceivedAt.Equal(t0) {
		t.Errorf("received_at = %v, want %v", j.ReceivedAt, t0)
	}
}

func TestPendingFinalsAndArchive(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "t.db"))
	s.Insert(newJob("done", time.Now()))
	s.Insert(newJob("busy", time.Now()))
	s.SetFinal("done", "FAILED", "paper out", "Bar", 3, time.Now())

	finals, err := s.PendingFinals()
	if err != nil || len(finals) != 1 || finals[0] != (Final{ID: "done", Status: "FAILED", Error: "paper out"}) {
		t.Fatalf("pending finals = %+v, %v", finals, err)
	}

	// archiving an unfinished job is a no-op
	if err := s.Archive("busy"); err != nil {
		t.Fatal(err)
	}
	if found, _, _ := s.Lookup("busy"); !found {
		t.Error("unfinished job was archived")
	}

	if err := s.Archive("done"); err != nil {
		t.Fatal(err)
	}
	if finals, _ := s.PendingFinals(); len(finals) != 0 {
		t.Errorf("archived job still pending: %+v", finals)
	}

	hist, err := s.History()
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %+v, %v", hist, err)
	}
	r := hist[0]
	if r.ID != "done" || r.Status != "FAILED" || r.Printer != "Bar" || r.Attempts != 3 || r.Error != "paper out" {
		t.Errorf("record = %+v", r)
	}
}

func TestHistoryTrim(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "t.db"))
	base := time.Now()

	for i := 0; i < historyLimit+5; i++ {
		id := fmt.Sprintf("job-%03d", i)
		s.Insert(newJob(id, base))
		s.SetFinal(id, "COMPLETED", "", "Dapur", 1, base.Add(time.Duration(i)*time.Millisecond))
		if err := s.Archive(id); err != nil {
			t.Fatal(err)
		}
	}

	hist, err := s.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != historyLimit {
		t.Fatalf("history len = %d, want %d", len(hist), historyLimit)
	}
	if hist[0].ID != fmt.Sprintf("job-%03d", historyLimit+4) {
		t.Errorf("newest = %s", hist[0].ID)
	}
}

func TestLookupFindsArchivedJob(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "t.db"))
	s.Insert(newJob("job-1", time.Now()))
	s.SetFinal("job-1", "COMPLETED", "", "Dapur", 1, time.Now())
	s.Archive("job-1")

	found, final, err := s.Lookup("job-1")
	if err != nil || !found || final == nil || final.Status != "COMPLETED" {
		t.Fatalf("archived lookup = %v, %+v, %v — an archived job must still dedupe", found, final, err)
	}
}
