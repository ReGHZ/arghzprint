// Package agent manages the connection to the backend and hands claimed print
// jobs to a Sink. It handles reconnection transparently so the rest of the app
// doesn't have to care about network state.
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

// daemonVersion is reported to the backend in printer.hello.
const daemonVersion = "0.1.0"

// StatusFunc is called whenever the connection state changes.
// Used to update the dashboard UI without coupling agent to the server.
type StatusFunc func(connected bool)

// Sink takes every job this agent claimed — new work or a redispatch of one
// already held locally; telling them apart is the sink's job
type Sink interface {
	Accept(ctx context.Context, j *job.Job)
}

// Agent connects to the backend, claims jobs, and hands them to the sink.
type Agent struct {
	cfg      *config.Manager
	sink     Sink
	onStatus StatusFunc
	sendCh   chan protocol.OutboundMessage // outbound hello/pong
}

func New(cfg *config.Manager, sink Sink, onStatus StatusFunc) *Agent {
	return &Agent{
		cfg:      cfg,
		sink:     sink,
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

// SendOut queues an outbound message (ack or hello) to the backend.
// Non-blocking — drops silently if the send buffer is full (reconnect re-syncs).
func (a *Agent) SendOut(msg protocol.OutboundMessage) {
	select {
	case a.sendCh <- msg:
	default:
		slog.Warn("send buffer full, dropping outbound", "event", msg.Event)
	}
}

// helloMessage builds the printer.hello identifying this daemon.
func (a *Agent) helloMessage() protocol.OutboundMessage {
	cfg := a.cfg.Get()

	enabled := make([]string, 0, len(cfg.EnabledTypes))
	for t, on := range cfg.EnabledTypes {
		if on {
			enabled = append(enabled, t)
		}
	}

	return protocol.OutboundMessage{
		Event: protocol.EventPrinterHello,
		Data: protocol.HelloData{
			AgentID:      cfg.AgentID,
			EnabledTypes: enabled,
			Version:      daemonVersion,
		},
	}
}

// claimAndAccept claims a job before handing it to the sink. Claiming is
// mandatory: without it, two daemons sharing a backend both print the same job.
func (a *Agent) claimAndAccept(ctx context.Context, env protocol.JobEnvelope) bool {
	cfg := a.cfg.Get()

	// enabled_types is an allowlist. A type that isn't enabled here is left
	// untouched — never claimed, never status-updated — so another agent that
	// does handle it can claim it. Canceling a job we don't own would kill it
	// for everyone. The backend also filters by hello.enabledTypes.
	if !cfg.EnabledTypes[env.Type] {
		slog.Debug("job type not enabled, skipping", "type", env.Type, "jobId", env.JobID)
		return false
	}

	claimed, err := a.ClaimJob(ctx, env.JobID)
	if err != nil {
		slog.Warn("claim failed", "jobId", env.JobID, "err", err)
		return false
	}
	if !claimed {
		slog.Debug("job claimed by another agent", "jobId", env.JobID)
		return false
	}

	j := job.FromEnvelope(env)

	// local priority_map wins when configured.
	// if not configured for this type, keep whatever the backend sent.
	// wulfcafe sends 0; backends that set meaningful priority still work.
	if p, ok := cfg.PriorityMap[env.Type]; ok {
		j.Priority = p
	}

	a.sink.Accept(ctx, j)
	return true
}
