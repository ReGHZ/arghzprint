package handler

import (
	"encoding/json"
	"net/http"
)

type testPrintRequest struct {
	Type    string `json:"type"`
	Printer string `json:"printer"`
}

func (h *Handler) TestPrint(w http.ResponseWriter, r *http.Request) {
	var req testPrintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.Type == "" || req.Printer == "" {
		http.Error(w, "type and printer are required", http.StatusBadRequest)
		return
	}

	if err := h.onTestPrint(req.Type, req.Printer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
