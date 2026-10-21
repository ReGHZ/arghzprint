package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ReGHZ/arghzprint/internal/config"
	tmpl "github.com/ReGHZ/arghzprint/internal/template"
)

func TestSaveTemplateValidatesType(t *testing.T) {
	dir := t.TempDir()
	store, err := tmpl.NewStore(filepath.Join(dir, "templates"))
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{store: store}
	r := chi.NewRouter()
	r.Put("/api/templates/{type}", h.SaveTemplate)

	cases := []struct {
		name     string
		path     string
		wantCode int
		wantFile string
	}{
		{"upper", "/api/templates/KITCHEN", http.StatusNoContent, "kitchen.html"},
		{"lower is uppercased", "/api/templates/invoice", http.StatusNoContent, "invoice.html"},
		{"underscore digits", "/api/templates/BAR_2", http.StatusNoContent, "bar_2.html"},
		{"dash rejected", "/api/templates/bad-name", http.StatusBadRequest, ""},
		{"dots rejected", "/api/templates/..", http.StatusBadRequest, ""},
		{"encoded slash rejected", "/api/templates/..%2Fescape", http.StatusBadRequest, ""},
		{"space rejected", "/api/templates/my%20type", http.StatusBadRequest, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader("<p>x</p>"))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantFile != "" {
				if _, err := os.Stat(filepath.Join(store.Dir(), tc.wantFile)); err != nil {
					t.Errorf("template file missing: %v", err)
				}
			}
		})
	}

	if _, err := os.Stat(filepath.Join(dir, "escape.html")); err == nil {
		t.Fatal("template written outside the template dir")
	}
}

func TestSaveSettingsValidatesNetworkPrinters(t *testing.T) {
	cfg, err := config.NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: cfg}

	bad := `{"network_printers":[{"name":"Dapur","host":"","port":9100,"width_dots":576}]}`
	rec := httptest.NewRecorder()
	h.SaveSettings(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(bad)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no host") {
		t.Fatalf("bad config: code=%d body=%q", rec.Code, rec.Body.String())
	}

	good := `{"network_printers":[{"name":"Dapur","host":"192.168.1.50"}]}`
	rec = httptest.NewRecorder()
	h.SaveSettings(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(good)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("good config: code=%d body=%q", rec.Code, rec.Body.String())
	}
	p, ok := cfg.Get().NetworkPrinter("Dapur")
	if !ok || p.Port != 9100 || p.WidthDots != 576 {
		t.Errorf("saved printer = %+v %v", p, ok)
	}
}
