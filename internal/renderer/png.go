package renderer

// PNGRenderer lays HTML out cssWidthPx wide (CSS px) and returns a PNG scaled to
// widthPx. Implemented outside Go — the Android WebView — and passed in via mobile.Start.
type PNGRenderer interface {
	RenderPNG(html string, cssWidthPx int, widthPx int) ([]byte, error)
}
