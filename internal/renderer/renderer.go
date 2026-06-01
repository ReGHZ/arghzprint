// Package renderer converts HTML strings to PDF bytes using wkhtmltopdf.
// Calls are serialized — wkhtmltopdf segfaults on some builds when invoked
// concurrently, and print jobs aren't time-critical enough to parallelize anyway.
package renderer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var mu sync.Mutex

// Renderer wraps a wkhtmltopdf binary.
type Renderer struct {
	binPath string
}

// New returns a Renderer using the given wkhtmltopdf binary path.
func New(binPath string) *Renderer {
	return &Renderer{binPath: binPath}
}

// Render converts HTML to PDF bytes.
// Returns an error immediately if wkhtmltopdf was not found at init time.
// Writes the HTML to a temp file because wkhtmltopdf reads from file, not stdin,
// when the page contains relative assets (fonts, images).
func (r *Renderer) Render(html string) ([]byte, error) {
	if r.binPath == "" {
		return nil, fmt.Errorf("wkhtmltopdf not installed — add binary to tools/unix/ or install via package manager")
	}

	mu.Lock()
	defer mu.Unlock()

	tmp, err := os.CreateTemp("", "arghzprint-*.html")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(html); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("write temp html: %w", err)
	}
	tmp.Close()

	outPath := tmp.Name() + ".pdf"
	defer os.Remove(outPath)

	var stderr bytes.Buffer
	cmd := exec.Command(r.binPath,
		"--quiet",
		"--page-width", "80mm",
		"--page-height", "297mm", // tall enough for any receipt; wkhtmltopdf clips content
		"--margin-top", "0",
		"--margin-bottom", "0",
		"--margin-left", "0",
		"--margin-right", "0",
		"--disable-smart-shrinking",
		tmp.Name(),
		outPath,
	)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("wkhtmltopdf: %w — %s", err, stderr.String())
	}

	// wkhtmltopdf exits 0 even on some failures; check stderr
	if stderr.Len() > 0 {
		// warnings are common and mostly harmless, only fail on "Error:"
		if bytes.Contains(stderr.Bytes(), []byte("Error:")) {
			return nil, fmt.Errorf("wkhtmltopdf error: %s", stderr.String())
		}
	}

	return os.ReadFile(outPath)
}

// BinPath returns the resolved path to the wkhtmltopdf binary.
func (r *Renderer) BinPath() string {
	return r.binPath
}

// FindOrExtract resolves the wkhtmltopdf binary.
// If an embedded binary was extracted to dataDir, use that.
// Otherwise fall back to whatever is on PATH.
func FindOrExtract(dataDir string, embedded []byte) (string, error) {
	extracted := filepath.Join(dataDir, "tools", wkhtmltopdfBin)

	if len(embedded) > 0 {
		if err := extractIfNeeded(extracted, embedded); err != nil {
			return "", err
		}
		return extracted, nil
	}

	// no embedded binary — require it on PATH
	path, err := exec.LookPath(wkhtmltopdfBin)
	if err != nil {
		return "", fmt.Errorf("wkhtmltopdf not found on PATH and no bundled binary: %w", err)
	}
	return path, nil
}

func extractIfNeeded(dst string, data []byte) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // already extracted
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create tools dir: %w", err)
	}

	if err := os.WriteFile(dst, data, 0755); err != nil {
		return fmt.Errorf("extract wkhtmltopdf: %w", err)
	}

	return nil
}
