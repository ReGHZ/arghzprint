package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ReGHZ/arghzprint/internal/agent"
	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/internal/printer"
	tmpl "github.com/ReGHZ/arghzprint/internal/template"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

type sent struct {
	id     string
	status protocol.JobStatus
}

// recorder stands in for the backend's PATCH endpoint
type recorder struct {
	mu     sync.Mutex
	sent   []sent
	down   bool
	delay  time.Duration
	reject map[protocol.JobStatus]bool
}

func (r *recorder) UpdateStatus(_ context.Context, id string, status protocol.JobStatus, _ string) error {
	time.Sleep(r.delay)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sent{id, status})
	if r.down {
		return errors.New("network down")
	}
	if r.reject[status] {
		return fmt.Errorf("%w: test", agent.ErrRejected)
	}
	return nil
}

func (r *recorder) statuses() []protocol.JobStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]protocol.JobStatus, len(r.sent))
	for i, s := range r.sent {
		out[i] = s.status
	}
	return out
}

func newTestApp(t *testing.T, mutate func(*config.Config)) (*App, *recorder) {
	t.Helper()
	dir := t.TempDir()

	cfg, err := config.NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Get()
	c.PrinterMap = map[string]string{"KITCHEN": "Dapur"}
	if mutate != nil {
		mutate(&c)
	}
	cfg.Save(c)

	st, err := job.OpenStore(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	rec := &recorder{reject: map[protocol.JobStatus]bool{}}
	return &App{
		cfg:      cfg,
		store:    st,
		status:   rec,
		lanes:    map[string]*lane{},
		progress: make(chan statusUpdate, 16),
		kick:     make(chan struct{}, 1),
	}, rec
}

// lanes never run in these tests: a cancelled ctx makes lane.run return at once,
// so whatever dispatch queued stays visible in lane.queue
func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func kitchenJob(id string) *job.Job {
	return &job.Job{ID: id, Type: "KITCHEN", Payload: map[string]any{"orderId": float64(1)}, ReceivedAt: time.Now()}
}

func queued(a *App, printerName string) int {
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()
	if l, ok := a.lanes[printerName]; ok {
		return l.queue.Len()
	}
	return 0
}

func equalStatuses(got []protocol.JobStatus, want ...protocol.JobStatus) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAcceptNewJob(t *testing.T) {
	a, rec := newTestApp(t, nil)
	a.Accept(cancelled(), kitchenJob("j1"))

	if !equalStatuses(rec.statuses(), protocol.JobStatusAcknowledged) {
		t.Errorf("sent = %v", rec.statuses())
	}
	if found, _, _ := a.store.Lookup("j1"); !found {
		t.Error("job not persisted")
	}
	if queued(a, "Dapur") != 1 {
		t.Errorf("queued = %d, want 1", queued(a, "Dapur"))
	}
}

func TestAcceptRedispatchOfHeldJobReacksOnly(t *testing.T) {
	a, rec := newTestApp(t, nil)
	a.Accept(cancelled(), kitchenJob("j1"))
	a.Accept(cancelled(), kitchenJob("j1"))

	if !equalStatuses(rec.statuses(), protocol.JobStatusAcknowledged, protocol.JobStatusAcknowledged) {
		t.Errorf("sent = %v", rec.statuses())
	}
	if queued(a, "Dapur") != 1 {
		t.Errorf("queued = %d, want 1 — redispatch must not print twice", queued(a, "Dapur"))
	}
}

func TestAcceptRedispatchOfFinishedJobResendsFinal(t *testing.T) {
	a, rec := newTestApp(t, nil)
	a.store.Insert(kitchenJob("j1"))
	a.store.SetFinal("j1", "COMPLETED", "", "Dapur", 1, time.Now())

	a.Accept(cancelled(), kitchenJob("j1"))

	// the backend reset the job to CLAIMED, so the final needs an ACK in front of it
	if !equalStatuses(rec.statuses(), protocol.JobStatusAcknowledged, protocol.JobStatusCompleted) {
		t.Errorf("sent = %v, want ACK then COMPLETED", rec.statuses())
	}
	if queued(a, "Dapur") != 0 {
		t.Error("finished job was queued for printing again")
	}
	if finals, _ := a.store.PendingFinals(); len(finals) != 0 {
		t.Error("job not archived after final delivered")
	}
}

// a lost ACK leaves the job CLAIMED: the final is 409'd and archived, the claim
// expires, and the backend hands the same job back — it must not print twice
func TestLostAckDoesNotReprint(t *testing.T) {
	a, rec := newTestApp(t, nil)
	rec.reject[protocol.JobStatusCompleted] = true

	j := kitchenJob("j1")
	a.Accept(cancelled(), j)
	a.finish(context.Background(), j, protocol.JobStatusCompleted, "", "Dapur")
	a.retryFinals(context.Background())
	if finals, _ := a.store.PendingFinals(); len(finals) != 0 {
		t.Fatalf("rejected final not archived: %v", finals)
	}

	a.lanes = map[string]*lane{}
	rec.reject = map[protocol.JobStatus]bool{}
	a.Accept(cancelled(), kitchenJob("j1"))

	if queued(a, "Dapur") != 0 {
		t.Fatal("redispatched job queued for printing again")
	}
}

func TestInsertFailureDoesNotAck(t *testing.T) {
	a, rec := newTestApp(t, nil)
	j := kitchenJob("j1")
	j.Payload["bad"] = func() {} // json.Marshal fails → Insert fails

	a.Accept(cancelled(), j)

	if len(rec.statuses()) != 0 {
		t.Errorf("sent = %v — no ACK may go out for a job that isn't persisted", rec.statuses())
	}
	if queued(a, "Dapur") != 0 {
		t.Error("unpersisted job was queued")
	}
}

func TestStartedRepairsLostAck(t *testing.T) {
	a, rec := newTestApp(t, nil)
	a.started(context.Background(), kitchenJob("j1"))
	a.flushProgress(context.Background())

	if !equalStatuses(rec.statuses(), protocol.JobStatusAcknowledged, protocol.JobStatusPrinting) {
		t.Errorf("sent = %v, want ACK then PRINTING", rec.statuses())
	}
}

func TestLaneHooksDoNotWaitForBackend(t *testing.T) {
	a, rec := newTestApp(t, nil)
	rec.delay = 2 * time.Second
	j := kitchenJob("j1")
	a.store.Insert(j)

	start := time.Now()
	a.started(context.Background(), j)
	a.finish(context.Background(), j, protocol.JobStatusCompleted, "", "Dapur")
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("lane hooks blocked %v on a slow backend", took)
	}
	if finals, _ := a.store.PendingFinals(); len(finals) != 1 {
		t.Errorf("final not persisted for later delivery: %v", finals)
	}
}

func TestFinishKeepsFinalWhileOffline(t *testing.T) {
	a, rec := newTestApp(t, nil)
	j := kitchenJob("j1")
	a.store.Insert(j)

	rec.down = true
	a.finish(context.Background(), j, protocol.JobStatusCompleted, "", "Dapur")
	a.retryFinals(context.Background())
	if finals, _ := a.store.PendingFinals(); len(finals) != 1 {
		t.Fatalf("pending finals = %v, want 1", finals)
	}

	rec.down = false
	a.retryFinals(context.Background())
	if finals, _ := a.store.PendingFinals(); len(finals) != 0 {
		t.Fatalf("pending finals after retry = %v", finals)
	}
	hist, _ := a.store.History()
	if len(hist) != 1 || hist[0].Status != "COMPLETED" || hist[0].Printer != "Dapur" {
		t.Errorf("history = %+v", hist)
	}
}

func TestFinishArchivesWhenBackendRejects(t *testing.T) {
	a, rec := newTestApp(t, nil)
	j := kitchenJob("j1")
	a.store.Insert(j)
	rec.reject[protocol.JobStatusCompleted] = true

	a.finish(context.Background(), j, protocol.JobStatusCompleted, "", "Dapur")
	a.retryFinals(context.Background())

	if finals, _ := a.store.PendingFinals(); len(finals) != 0 {
		t.Errorf("rejected final still pending: %v", finals)
	}
}

func TestDispatchWithoutPrinterFails(t *testing.T) {
	a, rec := newTestApp(t, func(c *config.Config) { c.PrinterMap = map[string]string{} })
	a.Accept(cancelled(), kitchenJob("j1"))
	a.retryFinals(context.Background())

	if !equalStatuses(rec.statuses(), protocol.JobStatusAcknowledged, protocol.JobStatusFailed) {
		t.Errorf("sent = %v", rec.statuses())
	}
	hist, _ := a.store.History()
	if len(hist) != 1 || hist[0].Status != "FAILED" {
		t.Errorf("history = %+v", hist)
	}
}

func TestResumeWorksWithBackendDown(t *testing.T) {
	a, rec := newTestApp(t, nil)
	rec.down = true
	a.store.Insert(kitchenJob("j1"))
	a.store.Insert(kitchenJob("j2"))

	a.resume(cancelled())

	if queued(a, "Dapur") != 2 {
		t.Errorf("queued = %d, want 2", queued(a, "Dapur"))
	}
	if len(rec.statuses()) != 0 {
		t.Errorf("resume talked to the backend: %v", rec.statuses())
	}
}

type fakePNG struct {
	mu       sync.Mutex
	css, dot int
	calls    int
}

func (f *fakePNG) RenderPNG(_ string, cssWidthPx, widthPx int) ([]byte, error) {
	f.mu.Lock()
	f.css, f.dot, f.calls = cssWidthPx, widthPx, f.calls+1
	f.mu.Unlock()

	img := image.NewGray(image.Rect(0, 0, widthPx, 4))
	for x := 0; x < widthPx; x++ {
		img.SetGray(x, 0, color.Gray{0})
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes(), nil
}

func TestAndroidPathPrintsOverTCP(t *testing.T) {
	addr := livePrinter(t)
	a, _ := newTestApp(t, func(c *config.Config) {
		c.PrintWidthMM = 72
		c.NetworkPrinters = []config.NetworkPrinter{{Name: "Dapur", Host: "127.0.0.1", Port: 9100, WidthDots: 576}}
	})

	store, err := tmpl.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.Save("KITCHEN", "<p>Order {{orderId}}</p>")
	a.engine = tmpl.NewEngine(store)

	cb := &fakePNG{}
	a.renderer = &pngRenderer{cb: cb, cfg: a.cfg}
	a.printer = printer.NewNetwork(func(name string) (string, bool) { return addr, name == "Dapur" }, func() []string { return []string{"Dapur"} })

	if err := a.printJob(kitchenJob("j1"), "Dapur"); err != nil {
		t.Fatal(err)
	}
	if cb.css != 272 || cb.dot != 576 {
		t.Errorf("RenderPNG got css=%d width=%d, want 272/576", cb.css, cb.dot)
	}

	err = a.printJob(kitchenJob("j2"), "Gone")
	if !isPermanent(err) || !errors.Is(err, printer.ErrUnknownPrinter) {
		t.Errorf("unknown printer err = %v, want permanent ErrUnknownPrinter", err)
	}
	if cb.calls != 1 {
		t.Errorf("renderer called %d times, want 1 (not for the unknown printer)", cb.calls)
	}
}

func TestTemplateErrorIsPermanent(t *testing.T) {
	a, _ := newTestApp(t, nil)
	store, _ := tmpl.NewStore(t.TempDir())
	store.Save("KITCHEN", "{{#if}}")
	a.engine = tmpl.NewEngine(store)

	if err := a.printJob(kitchenJob("j1"), "Dapur"); !isPermanent(err) {
		t.Errorf("err = %v, want permanent", err)
	}
}

func TestCSSWidth(t *testing.T) {
	cases := map[int]int{80: 302, 72: 272, 58: 219, 48: 181}
	for mm, want := range cases {
		if got := cssWidthPx(mm); got != want {
			t.Errorf("cssWidthPx(%d) = %d, want %d", mm, got, want)
		}
	}
}
