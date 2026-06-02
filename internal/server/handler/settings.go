package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ReGHZ/arghzprint/internal/config"
)

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.cfg.Get())
}

func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// agent_id is daemon-owned, not user-editable — a save must never wipe it,
	// otherwise it regenerates on next load and the backend loses claim ownership
	cfg.AgentID = h.cfg.Get().AgentID

	if err := h.cfg.Save(cfg); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListPrinters(w http.ResponseWriter, r *http.Request) {
	printers, err := h.printer.ListPrinters()
	if err != nil {
		http.Error(w, "failed to list printers", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"printers": printers})
}
