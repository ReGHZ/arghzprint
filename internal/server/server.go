package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/internal/printer"
	"github.com/ReGHZ/arghzprint/internal/server/handler"
	tmpl "github.com/ReGHZ/arghzprint/internal/template"
)

type Server struct {
	http *http.Server
}

func New(
	cfg *config.Manager,
	queue *job.Queue,
	store *tmpl.Store,
	engine *tmpl.Engine,
	p printer.Printer,
	connStatus func() bool,
	onTestPrint func(jobType, printerName string) error,
	jobHistory func() []job.Record,
) *Server {
	r := chi.NewRouter()
	r.Use(chimiddleware.Recoverer)
	r.Use(localhostOnly)
	r.Use(chimiddleware.RequestID)

	h := handler.New(cfg, queue, store, engine, p, connStatus, onTestPrint, jobHistory)

	r.Get("/health", h.Health)

	// static UI
	r.Get("/", h.UI)
	r.Get("/settings", h.UI)
	r.Get("/templates", h.UI)
	r.Get("/jobs", h.UI)
	r.Handle("/assets/*", h.Assets())

	// API
	r.Route("/api", func(r chi.Router) {
		r.Get("/status", h.Status)
		r.Get("/printers", h.ListPrinters)

		r.Get("/settings", h.GetSettings)
		r.Put("/settings", h.SaveSettings)

		r.Get("/templates", h.ListTemplates)
		r.Get("/templates/{type}", h.GetTemplate)
		r.Put("/templates/{type}", h.SaveTemplate)
		r.Delete("/templates/{type}", h.DeleteTemplate)

		r.Post("/preview", h.Preview)

		r.Get("/jobs", h.ListJobs)
		r.Post("/test-print", h.TestPrint)
	})

	port := cfg.Get().WebUIPort
	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: r,
	}

	return &Server{http: srv}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.http.Addr, err)
	}
	slog.Info("web UI available", "url", "http://"+s.http.Addr)
	go s.http.Serve(ln)
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// localhostOnly rejects any request that isn't from 127.0.0.1 or ::1.
// Belt-and-suspenders alongside binding to 127.0.0.1 only.
func localhostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
