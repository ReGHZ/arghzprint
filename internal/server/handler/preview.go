package handler

import (
	"encoding/json"
	"net/http"
)

type previewRequest struct {
	Template string         `json:"template"`
	Data     map[string]any `json:"data"`
}

// Preview renders a template string with provided sample data and returns HTML.
// Used by the template editor for live preview without saving.
func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.Template == "" {
		http.Error(w, "template is required", http.StatusBadRequest)
		return
	}

	html, err := h.engine.RenderRaw(req.Template, req.Data)
	if err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
