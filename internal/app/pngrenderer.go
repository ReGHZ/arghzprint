package app

import (
	"fmt"
	"math"
	"sync"

	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/printer"
	"github.com/ReGHZ/arghzprint/internal/renderer"
)

// pngRenderer lays the receipt out at the CSS width wkhtmltopdf uses (96 DPI)
// and lets the WebView scale it to the printer's dots, so templates look the
// same as on desktop
type pngRenderer struct {
	mu  sync.Mutex // one offscreen WebView at a time, even with several lanes
	cb  renderer.PNGRenderer
	cfg *config.Manager
}

func (r *pngRenderer) Render(html, printerName string) ([]byte, error) {
	c := r.cfg.Get()
	p, ok := c.NetworkPrinter(printerName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", printer.ErrUnknownPrinter, printerName)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	page, err := r.cb.RenderPNG(html, cssWidthPx(c.PrintWidthMM), p.WidthDots)
	if err != nil {
		return nil, fmt.Errorf("webview render: %w", err)
	}
	return page, nil
}

func cssWidthPx(mm int) int {
	return int(math.Round(float64(mm) * 96 / 25.4))
}
