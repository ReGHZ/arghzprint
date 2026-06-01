package job

import (
	"time"

	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

// Job is the internal representation of a print job in the working queue.
// It wraps the protocol envelope with runtime state.
type Job struct {
	ID       string
	Type     string
	Priority int
	Payload  map[string]any

	Attempts int
	LastErr  string

	ReceivedAt time.Time
}

// FromEnvelope converts a protocol job envelope into an internal Job.
func FromEnvelope(e protocol.JobEnvelope) *Job {
	return &Job{
		ID:         e.JobID,
		Type:       e.Type,
		Priority:   e.Priority,
		Payload:    e.Payload,
		ReceivedAt: time.Now(),
	}
}
