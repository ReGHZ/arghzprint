package handler

import (
	"encoding/json"
	"net/http"
	"runtime"
)

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"connected":  h.connStatus(),
		"queueDepth": h.queueDepth(),
		"printers":   h.printerStates(),
		// reporting only — the UI shows the Network Printers card on android
		"platform": runtime.GOOS,
	})
}
