package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

// fetchPending calls GET /api/printer/jobs/pending and enqueues anything
// that's still PENDING or DISPATCHED. Called on every (re)connect.
func (a *Agent) fetchPending(ctx context.Context) error {
	cfg := a.cfg.Get()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		cfg.BackendURL+"/api/printer/jobs/pending",
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.PrinterToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch pending: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("backend returned %d", resp.StatusCode)
	}

	var result protocol.PendingJobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parse pending jobs: %w", err)
	}

	for _, env := range result.Jobs {
		a.enqueue(env)
	}

	if len(result.Jobs) > 0 {
		slog.Info("recovered pending jobs", "count", len(result.Jobs))
	}

	return nil
}

// UpdateStatus sends a PATCH to the backend to update a job's status.
// Falls back gracefully — a failed update just means the backend stays stale,
// which is recoverable on next fetch.
func (a *Agent) UpdateStatus(ctx context.Context, jobID string, status protocol.JobStatus, jobErr string) {
	cfg := a.cfg.Get()

	body, _ := json.Marshal(protocol.StatusUpdateRequest{
		Status: status,
		Error:  jobErr,
	})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPatch,
		cfg.BackendURL+"/api/printer/jobs/"+jobID,
		bytes.NewReader(body),
	)
	if err != nil {
		slog.Warn("build status update request failed", "err", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.PrinterToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("status update failed", "jobId", jobID, "err", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		slog.Warn("backend rejected status update", "jobId", jobID, "status", resp.StatusCode)
	}
}

// RunPolling is the fallback when WebSocket is not available.
// It polls GET /pending every PollingIntervalSeconds and enqueues new jobs.
// Normally not used — WebSocket handles push. This exists for backends that
// don't implement the WS endpoint.
func (a *Agent) RunPolling(ctx context.Context) {
	cfg := a.cfg.Get()
	interval := time.Duration(cfg.PollingIntervalSeconds) * time.Second

	slog.Info("polling fallback active", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.fetchPending(ctx); err != nil {
				slog.Warn("poll failed", "err", err)
			}
		}
	}
}

