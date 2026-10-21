package app

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

type retryPolicy struct {
	base   time.Duration
	max    time.Duration
	maxAge time.Duration // older than this → FAILED instead of another retry
}

func defaultRetryPolicy(maxAgeMinutes int) retryPolicy {
	return retryPolicy{
		base:   5 * time.Second,
		max:    60 * time.Second,
		maxAge: time.Duration(maxAgeMinutes) * time.Minute,
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	return min(cur*2, max)
}

// laneHooks is what a lane needs from the app — an interface so lanes can be
// tested against a real TCP printer without building the whole app
type laneHooks interface {
	printJob(j *job.Job, printerName string) error
	started(ctx context.Context, j *job.Job)
	attempted(j *job.Job)
	finish(ctx context.Context, j *job.Job, status protocol.JobStatus, errMsg, printerName string)
}

// lane is one worker per printer, so a dead kitchen printer doesn't hold up the bar
type lane struct {
	name   string
	queue  *job.Queue
	hooks  laneHooks
	policy retryPolicy
	online atomic.Bool
	busy   atomic.Bool
}

func newLane(name string, hooks laneHooks, policy retryPolicy) *lane {
	l := &lane{name: name, queue: job.NewQueue(), hooks: hooks, policy: policy}
	l.online.Store(true)
	return l
}

func (l *lane) pending() int {
	n := l.queue.Len()
	if l.busy.Load() {
		n++
	}
	return n
}

func (l *lane) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		j := l.queue.Pop()
		if j == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}

		l.busy.Store(true)
		l.process(ctx, j)
		l.busy.Store(false)
	}
}

func (l *lane) process(ctx context.Context, j *job.Job) {
	l.hooks.started(ctx, j)

	backoff := l.policy.base
	for {
		err := l.hooks.printJob(j, l.name)
		if err == nil {
			l.online.Store(true)
			l.hooks.finish(ctx, j, protocol.JobStatusCompleted, "", l.name)
			return
		}

		j.Attempts++
		j.LastErr = err.Error()

		if isPermanent(err) {
			l.hooks.finish(ctx, j, protocol.JobStatusFailed, err.Error(), l.name)
			return
		}

		l.online.Store(false)
		l.hooks.attempted(j)
		slog.Warn("print attempt failed", "jobId", j.ID, "printer", l.name, "attempt", j.Attempts, "err", err)

		if time.Since(j.ReceivedAt) > l.policy.maxAge {
			l.hooks.finish(ctx, j, protocol.JobStatusFailed, err.Error(), l.name)
			return
		}

		select {
		case <-ctx.Done():
			// left unfinished in the store on purpose — resumed on next start
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, l.policy.max)
	}
}

// permanentError marks failures a retry can't fix (bad template, render crash),
// so the lane fails the job now instead of retrying for max_job_age_minutes
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return permanentError{err} }

func isPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}
