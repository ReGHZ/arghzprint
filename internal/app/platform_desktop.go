//go:build !android

package app

import (
	"log/slog"

	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/printer"
	"github.com/ReGHZ/arghzprint/internal/renderer"
	"github.com/ReGHZ/arghzprint/tools"
)

func newPlatform(dataDir string, cfg *config.Manager, _ renderer.PNGRenderer) (pageRenderer, printer.Printer) {
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

	return pdfRenderer{r: renderer.New(rendererBin, cfg.Get().PrintWidthMM)}, printer.New(sumatraBin)
}

type pdfRenderer struct {
	r *renderer.Renderer
}

func (p pdfRenderer) Render(html, _ string) ([]byte, error) {
	return p.r.Render(html)
}
