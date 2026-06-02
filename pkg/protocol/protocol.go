// Package protocol defines the contract between arghzprint and any backend.
// Backends conforming to this protocol can send print jobs without knowing
// anything about templates, printers, or rendering.
package protocol

// JobStatus is the lifecycle state of a print job.
// Backends track this to know whether a job was actually printed.
type JobStatus string

const (
	JobStatusPending      JobStatus = "PENDING"
	JobStatusDispatched   JobStatus = "DISPATCHED"
	JobStatusAcknowledged JobStatus = "ACKNOWLEDGED"
	JobStatusPrinting     JobStatus = "PRINTING"
	JobStatusCompleted    JobStatus = "COMPLETED"
	JobStatusFailed       JobStatus = "FAILED"
	JobStatusCanceled     JobStatus = "CANCELED"
)

// Event names over the WebSocket channel.
// Status updates are not sent over the WebSocket — they go via PATCH (see
// StatusUpdateRequest), so the daemon → backend WS traffic is only hello and pong.
const (
	EventPrinterHello = "printer.hello" // daemon → backend: identify on connect
	EventPrintJob     = "print.job"     // backend → daemon: new job
	EventPing         = "ping"
	EventPong         = "pong"
)

// InboundMessage is the WebSocket envelope from backend → daemon.
type InboundMessage struct {
	Event string      `json:"event"`
	Data  JobEnvelope `json:"data"`
}

// JobEnvelope carries a single print job.
// Type is a free-form string — the daemon maps it to a template file.
// Payload is arbitrary JSON; template variables reference its keys directly.
// Priority is optional — daemon's priority_map takes precedence if configured.
// Backends that don't manage priority can omit it (defaults to 0).
type JobEnvelope struct {
	JobID    string         `json:"jobId"`
	Type     string         `json:"type"`
	Priority int            `json:"priority,omitempty"`
	Payload  map[string]any `json:"payload"`
}

// OutboundMessage is the WebSocket envelope from daemon → backend.
// Data is HelloData for printer.hello; pong carries no data.
type OutboundMessage struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

// HelloData identifies the daemon to the backend immediately after connect,
// so the backend only dispatches jobs for types this agent handles.
type HelloData struct {
	AgentID      string   `json:"agentId"`
	EnabledTypes []string `json:"enabledTypes"`
	Version      string   `json:"version"`
}

// PendingJobsResponse is the shape of GET /api/printer/jobs/pending.
// Called on startup and reconnect to recover jobs that arrived while offline.
type PendingJobsResponse struct {
	Jobs []JobEnvelope `json:"jobs"`
}

// StatusUpdateRequest is the body of PATCH /api/printer/jobs/:id.
// AgentID lets the backend verify the update comes from the agent that claimed the job.
type StatusUpdateRequest struct {
	AgentID string    `json:"agentId"`
	Status  JobStatus `json:"status"`
	Error   string    `json:"error,omitempty"`
}

// ClaimRequest is the body of POST /api/printer/jobs/:id/claim.
type ClaimRequest struct {
	AgentID string `json:"agentId"`
}

// ClaimResponse reports whether this agent won the claim. A false value
// (or a 409) means another agent already owns the job.
type ClaimResponse struct {
	Claimed bool `json:"claimed"`
}
