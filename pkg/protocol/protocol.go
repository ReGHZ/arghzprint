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
const (
	EventPrintJob = "print.job" // backend → daemon: new job
	EventPrintAck = "print.ack" // daemon → backend: status update
	EventPing     = "ping"
	EventPong     = "pong"
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
type OutboundMessage struct {
	Event string    `json:"event"`
	Data  AckData   `json:"data"`
}

// AckData is sent after each status transition.
type AckData struct {
	JobID  string    `json:"jobId"`
	Status JobStatus `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// PendingJobsResponse is the shape of GET /api/printer/jobs/pending.
// Called on startup and reconnect to recover jobs that arrived while offline.
type PendingJobsResponse struct {
	Jobs []JobEnvelope `json:"jobs"`
}

// StatusUpdateRequest is the body of PATCH /api/printer/jobs/:id.
type StatusUpdateRequest struct {
	Status JobStatus `json:"status"`
	Error  string    `json:"error,omitempty"`
}
