package handler

import (
	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/job"
	"github.com/ReGHZ/arghzprint/internal/printer"
	tmpl "github.com/ReGHZ/arghzprint/internal/template"
)

// Handler holds dependencies for all HTTP handlers.
type Handler struct {
	cfg           *config.Manager
	store         *tmpl.Store
	engine        *tmpl.Engine
	printer       printer.Printer
	connStatus    func() bool
	queueDepth    func() int
	printerStates func() []job.LaneState
	onTestPrint   func(jobType, printerName string) error
	jobHistory    func() []job.Record
}

func New(
	cfg *config.Manager,
	store *tmpl.Store,
	engine *tmpl.Engine,
	p printer.Printer,
	connStatus func() bool,
	queueDepth func() int,
	printerStates func() []job.LaneState,
	onTestPrint func(jobType, printerName string) error,
	jobHistory func() []job.Record,
) *Handler {
	return &Handler{
		cfg:           cfg,
		store:         store,
		engine:        engine,
		printer:       p,
		connStatus:    connStatus,
		queueDepth:    queueDepth,
		printerStates: printerStates,
		onTestPrint:   onTestPrint,
		jobHistory:    jobHistory,
	}
}
