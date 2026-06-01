// Package agent manages the connection to the backend and feeds incoming
// print jobs into the queue. It handles reconnection transparently so the
// rest of the app doesn't have to care about network state.
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

// StatusFunc is called whenever the connection state changes.
// Used to update the dashboard UI without coupling agent to the server.
type StatusFunc func(connected bool)

// Agent connects to the backend, receives jobs, and enqueues them.
type Agent struct {
	cfg      *config.Manager
	queue    *job.Queue
	onStatus StatusFunc
	sendCh   chan protocol.OutboundMessage // outbound ACKs
}

func New(cfg *config.Manager, q *job.Queue, onStatus StatusFunc) *Agent {
	return &Agent{
		cfg:      cfg,
		queue:    q,
		onStatus: onStatus,
		sendCh:   make(chan protocol.OutboundMessage, 64),
	}
}

// Run connects to the backend and keeps the connection alive until ctx is cancelled.
// It blocks — run it in a goroutine.
func (a *Agent) Run(ctx context.Context) {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		slog.Info("connecting to backend", "url", a.cfg.Get().BackendURL)

		err := a.runWebSocket(ctx)
		if ctx.Err() != nil {
			return
		}

		if a.onStatus != nil {
			a.onStatus(false)
		}

		slog.Warn("disconnected, retrying", "backoff", backoff, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// cap backoff at 30s
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// SendAck queues an outbound ACK to the backend.
// Non-blocking — drops silently if the send buffer is full (reconnect will re-sync via polling).
func (a *Agent) SendAck(msg protocol.OutboundMessage) {
	select {
	case a.sendCh <- msg:
	default:
		slog.Warn("send buffer full, dropping ack", "jobId", msg.Data.JobID)
	}
}

// enqueue converts an envelope to a Job and pushes it into the working queue.
func (a *Agent) enqueue(env protocol.JobEnvelope) {
	cfg := a.cfg.Get()

	if enabled, ok := cfg.EnabledTypes[env.Type]; ok && !enabled {
		slog.Debug("job type disabled, skipping", "type", env.Type, "jobId", env.JobID)
		return
	}

	j := job.FromEnvelope(env)

	// priority is owned by this daemon, not the backend.
	// backend sends 0; local config decides processing order.
	if p, ok := cfg.PriorityMap[env.Type]; ok {
		j.Priority = p
	} else {
		j.Priority = 0
	}

	a.queue.Push(j)
	slog.Info("job enqueued", "id", j.ID, "type", j.Type, "priority", j.Priority)
}
