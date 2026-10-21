package app

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/ReGHZ/arghzprint/internal/agent"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

const finalsRetryInterval = 30 * time.Second

// Accept handles a job this agent just claimed. A jobId already in the store
// means the backend handed back something we still hold or already printed —
// re-ACK it (and re-send its final status) instead of printing it twice.
func (a *App) Accept(ctx context.Context, j *job.Job) {
	found, final, err := a.store.Lookup(j.ID)
	if err != nil {
		// no ACK: the claim expires and the backend redispatches it
		slog.Error("job lookup failed", "jobId", j.ID, "err", err)
		return
	}
	if final != nil {
		// the job is CLAIMED again on the backend; a final straight from CLAIMED is an invalid transition
		a.ack(ctx, j.ID)
		a.deliverFinal(ctx, *final)
		return
	}
	if found {
		a.ack(ctx, j.ID)
		slog.Info("redispatched job already held, re-acked", "jobId", j.ID)
		return
	}

	// persist before ACK: the watchdog only resets un-ACKed jobs, so an ACKed
	// job that never reached the store would be lost for good
	inserted, err := a.store.Insert(j)
	if err != nil {
		slog.Error("persist job failed", "jobId", j.ID, "err", err)
		return
	}
	if !inserted {
		return
	}

	a.ack(ctx, j.ID)
	slog.Info("job accepted", "id", j.ID, "type", j.Type, "priority", j.Priority)
	a.dispatch(ctx, j)
}

func (a *App) ack(ctx context.Context, id string) {
	if err := a.status.UpdateStatus(ctx, id, protocol.JobStatusAcknowledged, ""); err != nil {
		// started() sends it again, and a later re-claim repairs it too
		slog.Warn("ack not delivered", "jobId", id, "err", err)
	}
}

// resume re-queues jobs a killed process left behind. They're already claimed
// by this agent, so no claim and no backend round-trip — works offline too.
func (a *App) resume(ctx context.Context) {
	jobs, err := a.store.Unfinished()
	if err != nil {
		slog.Error("load unfinished jobs", "err", err)
		return
	}
	for _, j := range jobs {
		a.dispatch(ctx, j)
	}
	if len(jobs) > 0 {
		slog.Info("resumed unfinished jobs", "count", len(jobs))
	}
}

func (a *App) dispatch(ctx context.Context, j *job.Job) {
	printerName := a.cfg.Get().PrinterMap[j.Type]
	if printerName == "" {
		a.finish(ctx, j, protocol.JobStatusFailed, "no printer configured for type: "+j.Type, "")
		return
	}
	a.laneFor(ctx, printerName).queue.Push(j)
}

// lanes start lazily on the ctx of whoever dispatched first — always Run's ctx
func (a *App) laneFor(ctx context.Context, printerName string) *lane {
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()

	if l, ok := a.lanes[printerName]; ok {
		return l
	}
	l := newLane(printerName, a, defaultRetryPolicy(a.cfg.Get().MaxJobAgeMinutes))
	a.lanes[printerName] = l
	a.wg.Go(func() { l.run(ctx) })
	return l
}

// started and finish run on a lane — they only queue backend calls, so a slow
// or unreachable backend never holds up printing
func (a *App) started(_ context.Context, j *job.Job) {
	// ACK again first: if the one from Accept was lost the job is still CLAIMED,
	// and PRINTING/COMPLETED would be rejected as invalid transitions
	a.queueStatus(statusUpdate{id: j.ID, status: protocol.JobStatusAcknowledged})
	a.queueStatus(statusUpdate{id: j.ID, status: protocol.JobStatusPrinting})
}

func (a *App) attempted(j *job.Job) {
	if err := a.store.RecordAttempt(j.ID, j.Attempts, j.LastErr); err != nil {
		slog.Warn("persist attempt failed", "jobId", j.ID, "err", err)
	}
}

func (a *App) finish(_ context.Context, j *job.Job, status protocol.JobStatus, errMsg, printerName string) {
	if err := a.store.SetFinal(j.ID, string(status), errMsg, printerName, j.Attempts, time.Now()); err != nil {
		slog.Error("persist final status failed", "jobId", j.ID, "err", err)
	}

	if status == protocol.JobStatusFailed {
		slog.Error("job failed", "jobId", j.ID, "type", j.Type, "printer", printerName, "err", errMsg)
	} else {
		slog.Info("job printed", "jobId", j.ID, "type", j.Type, "printer", printerName)
	}

	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// deliverFinal archives once the backend has the final status. A 4xx means it
// never will (job canceled meanwhile) — archive anyway instead of retrying forever.
func (a *App) deliverFinal(ctx context.Context, f job.Final) {
	err := a.status.UpdateStatus(ctx, f.ID, protocol.JobStatus(f.Status), f.Error)
	if err != nil && !errors.Is(err, agent.ErrRejected) {
		slog.Warn("final status not delivered, will retry", "jobId", f.ID, "err", err)
		return
	}
	if err != nil {
		slog.Warn("backend rejected final status, archiving anyway", "jobId", f.ID, "err", err)
	}

	if err := a.store.Archive(f.ID); err != nil {
		slog.Error("archive job failed", "jobId", f.ID, "err", err)
	}
}

type statusUpdate struct {
	id     string
	status protocol.JobStatus
}

// progress updates (ACK/PRINTING) are best effort — dropping one is repaired by
// the next ACK or the final status, so never block a lane on a full buffer
func (a *App) queueStatus(u statusUpdate) {
	select {
	case a.progress <- u:
	default:
		slog.Warn("status buffer full, dropping progress update", "jobId", u.id, "status", u.status)
	}
}

func (a *App) flushProgress(ctx context.Context) {
	for {
		select {
		case u := <-a.progress:
			a.sendProgress(ctx, u)
		default:
			return
		}
	}
}

func (a *App) retryFinals(ctx context.Context) {
	finals, err := a.store.PendingFinals()
	if err != nil {
		slog.Warn("load pending finals", "err", err)
		return
	}
	for _, f := range finals {
		a.deliverFinal(ctx, f)
	}
}

// runStatus is the only goroutine talking status to the backend for lanes.
// Progress is flushed before finals so the backend sees ACK → PRINTING → final.
func (a *App) runStatus(ctx context.Context) {
	a.retryFinals(ctx)

	t := time.NewTicker(finalsRetryInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-a.progress:
			a.sendProgress(ctx, u)
		case <-a.kick:
			a.flushProgress(ctx)
			a.retryFinals(ctx)
		case <-t.C:
			a.flushProgress(ctx)
			a.retryFinals(ctx)
		}
	}
}

func (a *App) sendProgress(ctx context.Context, u statusUpdate) {
	if err := a.status.UpdateStatus(ctx, u.id, u.status, ""); err != nil {
		slog.Debug("progress status not delivered", "jobId", u.id, "status", u.status, "err", err)
	}
}

func (a *App) laneStates() []job.LaneState {
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()

	out := make([]job.LaneState, 0, len(a.lanes))
	for name, l := range a.lanes {
		out = append(out, job.LaneState{Name: name, Online: l.online.Load(), Pending: l.pending()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (a *App) queueDepth() int {
	n := 0
	for _, s := range a.laneStates() {
		n += s.Pending
	}
	return n
}

func (a *App) history() []job.Record {
	recs, err := a.store.History()
	if err != nil {
		slog.Warn("read job history", "err", err)
	}
	return recs
}
