// Package app wires all components together and manages the application lifecycle.
package app

import (
	"context"
	"errors"
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
)

// pageRenderer turns HTML into what the platform printer takes:
// PDF for the desktop spooler, PNG for raw ESC/POS on Android
type pageRenderer interface {
	Render(html, printerName string) ([]byte, error)
}

type statusSender interface {
	UpdateStatus(ctx context.Context, jobID string, status protocol.JobStatus, jobErr string) error
}

// Options lets non-desktop shells supply what the OS can't.
type Options struct {
	DataDir string               // empty = OS default data dir
	PNG     renderer.PNGRenderer // Android only: HTML → PNG via WebView
}

type App struct {
	cfg       *config.Manager
	store     *job.Store
	agent     *agent.Agent
	status    statusSender
	engine    *tmpl.Engine
	renderer  pageRenderer
	printer   printer.Printer
	server    *server.Server
	connected atomic.Bool

	lanesMu sync.Mutex
	lanes   map[string]*lane
	wg      sync.WaitGroup

	progress chan statusUpdate
	kick     chan struct{}
}

func New() (*App, error) {
	return NewWithOptions(Options{})
}

func NewWithOptions(o Options) (*App, error) {
	dataDir := o.DataDir
	if dataDir == "" {
		d, err := config.DataDir()
		if err != nil {
			return nil, err
		}
		dataDir = d
	}

	cfg, err := config.NewAt(dataDir)
	if err != nil {
		return nil, err
	}

	templates, err := tmpl.NewStore(filepath.Join(dataDir, "templates"))
	if err != nil {
		return nil, err
	}
	if err := seedDefaults(templates); err != nil {
		slog.Warn("failed to seed default templates", "err", err)
	}

	jobs, err := job.OpenStore(filepath.Join(dataDir, "arghzprint.db"))
	if err != nil {
		return nil, err
	}

	rdr, p := newPlatform(dataDir, cfg, o.PNG)

	a := &App{
		cfg:      cfg,
		store:    jobs,
		engine:   tmpl.NewEngine(templates),
		renderer: rdr,
		printer:  p,
		lanes:    map[string]*lane{},
		progress: make(chan statusUpdate, 256),
		kick:     make(chan struct{}, 1),
	}
	a.agent = agent.New(cfg, a, func(connected bool) {
		a.connected.Store(connected)
	})
	a.status = a.agent

	a.server = server.New(cfg, templates, a.engine, p,
		func() bool { return a.connected.Load() },
		a.queueDepth,
		a.laneStates,
		a.testPrint,
		a.history,
	)

	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	defer a.store.Close()

	if err := a.server.Start(); err != nil {
		return err
	}

	a.resume(ctx)

	if a.cfg.Get().ConnectionMode == "polling" {
		a.wg.Go(func() { a.agent.RunPolling(ctx) })
	} else {
		a.wg.Go(func() { a.agent.Run(ctx) })
	}
	a.wg.Go(func() { a.runStatus(ctx) })

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.server.Shutdown(shutdownCtx)

	a.wg.Wait()
	return nil
}

// printJob classifies failures: anything before the printer is permanent,
// only the printer itself (network, paper) is worth retrying
func (a *App) printJob(j *job.Job, printerName string) error {
	html, err := a.engine.Render(j.Type, j.Payload)
	if err != nil {
		return permanent(err)
	}

	page, err := a.renderer.Render(html, printerName)
	if err != nil {
		return permanent(err)
	}

	err = a.printer.Print(page, printerName)
	if errors.Is(err, printer.ErrUnknownPrinter) {
		return permanent(err)
	}
	return err
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

	page, err := a.renderer.Render(html, printerName)
	if err != nil {
		return fmt.Errorf("render test page: %w", err)
	}

	return a.printer.Print(page, printerName)
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
