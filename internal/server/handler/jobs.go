package handler

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"queueDepth": h.queue.Len(),
		"records":    h.jobHistory(),
	})
}
