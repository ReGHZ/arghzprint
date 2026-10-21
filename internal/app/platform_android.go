//go:build android

package app

import (
	"github.com/ReGHZ/arghzprint/internal/config"
	"github.com/ReGHZ/arghzprint/internal/printer"
	"github.com/ReGHZ/arghzprint/internal/renderer"
)

// no tools import here on purpose — it would embed the desktop wkhtmltopdf binary into the .aar
func newPlatform(_ string, cfg *config.Manager, png renderer.PNGRenderer) (pageRenderer, printer.Printer) {
	lookup := func(name string) (string, bool) {
		p, ok := cfg.Get().NetworkPrinter(name)
		return p.Addr(), ok
	}
	names := func() []string {
		ps := cfg.Get().NetworkPrinters
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.Name
		}
		return out
	}
	return &pngRenderer{cb: png, cfg: cfg}, printer.NewNetwork(lookup, names)
}
