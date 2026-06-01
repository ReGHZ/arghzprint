package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	tmpl "github.com/ReGHZ/arghzprint/internal/template"
)

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	types, err := h.store.List()
	if err != nil {
		http.Error(w, "failed to list templates", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"types": types})
}

func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	jobType := strings.ToUpper(chi.URLParam(r, "type"))

	html, err := h.store.Get(jobType)
	if errors.Is(err, tmpl.ErrNotFound) {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to read template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (h *Handler) SaveTemplate(w http.ResponseWriter, r *http.Request) {
	jobType := strings.ToUpper(chi.URLParam(r, "type"))

	body, err := io.ReadAll(io.LimitReader(r.Body, 512*1024)) // 512KB max
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if err := h.store.Save(jobType, string(body)); err != nil {
		http.Error(w, "failed to save template", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	jobType := strings.ToUpper(chi.URLParam(r, "type"))

	if err := h.store.Delete(jobType); err != nil {
		http.Error(w, "failed to delete template", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
