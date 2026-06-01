// Package app wires all components together and manages the application lifecycle.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ReGHZ/arghzprint/internal/agent"
	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/internal/printer"
	"github.com/ReGHZ/arghzprint/internal/renderer"
	"github.com/ReGHZ/arghzprint/internal/server"
	tmpl "github.com/ReGHZ/arghzprint/internal/template"
	"github.com/ReGHZ/arghzprint/pkg/protocol"
	defaulttemplates "github.com/ReGHZ/arghzprint/templates"
	"github.com/ReGHZ/arghzprint/tools"
)

const retryInterval = 3 * time.Second

type App struct {
	cfg       *config.Manager
	queue     *job.Queue
	history   *job.History
	agent     *agent.Agent
	engine    *tmpl.Engine
	renderer  *renderer.Renderer
	printer   printer.Printer
	server    *server.Server
	connected atomic.Bool
}

func New() (*App, error) {
	cfg, err := config.New()
	if err != nil {
		return nil, err
	}

	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}

	// tool binaries are optional at startup — missing tools are caught at print time
	rendererBin, err := renderer.FindOrExtract(dataDir, tools.WkhtmltopdfBin())
	if err != nil {
		slog.Warn("wkhtmltopdf not available, printing will fail until installed", "err", err)
		rendererBin = ""
	}

	sumatraBin, err := printer.FindOrExtract(dataDir, tools.SumatraBin())
	if err != nil {
		slog.Warn("SumatraPDF not available", "err", err)
		sumatraBin = ""
	}

	templateDir := filepath.Join(dataDir, "templates")
	store, err := tmpl.NewStore(templateDir)
	if err != nil {
		return nil, err
	}

	// seed default templates if the directory was just created
	if err := seedDefaults(store); err != nil {
		slog.Warn("failed to seed default templates", "err", err)
	}

	q := job.NewQueue()
	h := job.NewHistory(100)
	engine := tmpl.NewEngine(store)
	rdr := renderer.New(rendererBin)
	p := printer.New(sumatraBin)

	a := &App{
		cfg:      cfg,
		queue:    q,
		history:  h,
		engine:   engine,
		renderer: rdr,
		printer:  p,
	}

	a.agent = agent.New(cfg, q, func(connected bool) {
		a.connected.Store(connected)
	})

	a.server = server.New(cfg, q, store, engine, p,
		func() bool { return a.connected.Load() },
		a.testPrint,
		h.All,
	)

	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.server.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	if a.cfg.Get().ConnectionMode == "polling" {
		wg.Go(func() { a.agent.RunPolling(ctx) })
	} else {
		wg.Go(func() { a.agent.Run(ctx) })
	}
	wg.Go(func() { a.runWorker(ctx) })

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.server.Shutdown(shutdownCtx)

	wg.Wait()
	return nil
}

// runWorker drains the queue and processes each job.
func (a *App) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		j := a.queue.Pop()
		if j == nil {
			// empty queue — small sleep to avoid busy loop
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}

		a.process(ctx, j)
	}
}

func (a *App) process(ctx context.Context, j *job.Job) {
	cfg := a.cfg.Get()

	printerName, ok := cfg.PrinterMap[j.Type]
	if !ok || printerName == "" {
		slog.Warn("no printer configured for type", "type", j.Type, "jobId", j.ID)
		a.agent.UpdateStatus(ctx, j.ID, protocol.JobStatusFailed, "no printer configured for type: "+j.Type)
		return
	}

	a.agent.UpdateStatus(ctx, j.ID, protocol.JobStatusPrinting, "")

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := a.printJob(j, printerName); err != nil {
			lastErr = err
			j.Attempts++
			j.LastErr = err.Error()
			slog.Warn("print attempt failed", "jobId", j.ID, "attempt", attempt, "err", err)

			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return
				case <-time.After(retryInterval):
				}
			}
			continue
		}

		slog.Info("job printed", "jobId", j.ID, "type", j.Type, "printer", printerName)
		a.agent.UpdateStatus(ctx, j.ID, protocol.JobStatusCompleted, "")
		a.history.Add(job.Record{
			ID:          j.ID,
			Type:        j.Type,
			Status:      string(protocol.JobStatusCompleted),
			Printer:     printerName,
			Attempts:    j.Attempts,
			ReceivedAt:  j.ReceivedAt,
			CompletedAt: time.Now(),
		})
		return
	}

	slog.Error("job failed after retries", "jobId", j.ID, "err", lastErr)
	a.agent.UpdateStatus(ctx, j.ID, protocol.JobStatusFailed, lastErr.Error())
	a.history.Add(job.Record{
		ID:          j.ID,
		Type:        j.Type,
		Status:      string(protocol.JobStatusFailed),
		Printer:     printerName,
		Attempts:    j.Attempts,
		Error:       lastErr.Error(),
		ReceivedAt:  j.ReceivedAt,
		CompletedAt: time.Now(),
	})
}

// seedDefaults copies built-in templates to the user's template directory
// only if the file doesn't already exist (never overwrites customizations).
func seedDefaults(store *tmpl.Store) error {
	entries, err := defaulttemplates.FS.ReadDir(".")
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		// strip .html to get the type name
		typeName := strings.ToUpper(strings.TrimSuffix(name, ".html"))

		// don't overwrite if user already has a template for this type
		if _, err := store.Get(typeName); err == nil {
			continue
		}

		data, err := defaulttemplates.FS.ReadFile(name)
		if err != nil {
			return err
		}

		if err := store.Save(typeName, string(data)); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) testPrint(jobType, printerName string) error {
	sample := map[string]any{
		"orderId":      "TEST",
		"orderToken":   "T1",
		"tableLabels":  []string{"TEST"},
		"customerName": "Test Print",
		"serviceType":  "DINE_IN",
		"station":      jobType,
		"items": []map[string]any{
			{"name": "Test Item", "qty": 1, "notes": "test print — ignore"},
		},
		"total":     "Rp 0",
		"timestamp": "test",
	}

	html, err := a.engine.Render(jobType, sample)
	if err != nil {
		return fmt.Errorf("render test template: %w", err)
	}

	pdf, err := a.renderer.Render(html)
	if err != nil {
		return fmt.Errorf("render test pdf: %w", err)
	}

	return a.printer.Print(pdf, printerName)
}

func (a *App) printJob(j *job.Job, printerName string) error {
	html, err := a.engine.Render(j.Type, j.Payload)
	if err != nil {
		return err
	}

	pdf, err := a.renderer.Render(html)
	if err != nil {
		return err
	}

	return a.printer.Print(pdf, printerName)
}
