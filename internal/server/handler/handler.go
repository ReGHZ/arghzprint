package handler

import (
	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/internal/printer"
	tmpl "github.com/ReGHZ/arghzprint/internal/template"
)

// Handler holds dependencies for all HTTP handlers.
type Handler struct {
	cfg         *config.Manager
	queue       *job.Queue
	store       *tmpl.Store
	engine      *tmpl.Engine
	printer     printer.Printer
	connStatus  func() bool
	onTestPrint func(jobType, printerName string) error
	jobHistory  func() []job.Record
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
) *Handler {
	return &Handler{
		cfg:         cfg,
		queue:       queue,
		store:       store,
		engine:      engine,
		printer:     p,
		connStatus:  connStatus,
		onTestPrint: onTestPrint,
		jobHistory:  jobHistory,
	}
}
