package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

// fetchPending calls GET /api/printer/jobs/pending and accepts anything
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

	resp, err := httpClient.Do(req)
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

	accepted := 0
	for _, env := range result.Jobs {
		if a.claimAndAccept(ctx, env) {
			accepted++
		}
	}

	if accepted > 0 {
		slog.Info("recovered pending jobs", "count", accepted)
	}

	return nil
}

// ClaimJob calls POST /api/printer/jobs/:id/claim. It reports whether this
// agent won the claim — a false result (or 409 Conflict) means another agent
// already owns the job and we must not accept or print it.
func (a *Agent) ClaimJob(ctx context.Context, jobID string) (bool, error) {
	cfg := a.cfg.Get()

	body, _ := json.Marshal(protocol.ClaimRequest{AgentID: cfg.AgentID})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cfg.BackendURL+"/api/printer/jobs/"+jobID+"/claim",
		bytes.NewReader(body),
	)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.PrinterToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("claim job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("backend returned %d", resp.StatusCode)
	}

	var result protocol.ClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("parse claim response: %w", err)
	}
	return result.Claimed, nil
}

// http.DefaultClient has no timeout — a blackholed backend would hang a call forever
var httpClient = &http.Client{Timeout: 10 * time.Second}

// ErrRejected means the backend refused the update for good (bad request, job
// gone, invalid transition) — sending it again won't change its mind. Auth and
// rate-limit errors are not rejections: they clear up once the token is fixed.
var ErrRejected = errors.New("backend rejected status update")

// UpdateStatus sends PATCH /api/printer/jobs/:id. Network errors and 5xx are
// worth retrying, and so are 401/403/429; ErrRejected is not.
func (a *Agent) UpdateStatus(ctx context.Context, jobID string, status protocol.JobStatus, jobErr string) error {
	cfg := a.cfg.Get()

	body, _ := json.Marshal(protocol.StatusUpdateRequest{
		AgentID: cfg.AgentID,
		Status:  status,
		Error:   jobErr,
	})

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPatch,
		cfg.BackendURL+"/api/printer/jobs/"+jobID,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build status update %s: %w", jobID, err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.PrinterToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("status update %s: %w", jobID, err)
	}
	resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent:
		return nil
	case resp.StatusCode == http.StatusBadRequest,
		resp.StatusCode == http.StatusNotFound,
		resp.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: %s %s → %d", ErrRejected, jobID, status, resp.StatusCode)
	default:
		return fmt.Errorf("status update %s: backend returned %d", jobID, resp.StatusCode)
	}
}

// RunPolling is the fallback when WebSocket is not available.
// It polls GET /pending every PollingIntervalSeconds and accepts new jobs.
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
