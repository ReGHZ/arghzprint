//go:build windows

package printer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WindowsPrinter uses SumatraPDF for silent PDF printing.
// SumatraPDF exits 0 on success and non-zero on failure, but also writes
// error messages to stderr — check both.
type WindowsPrinter struct {
	sumatraBin string
}

func New(sumatraBin string) Printer {
	return &WindowsPrinter{sumatraBin: sumatraBin}
}

func (p *WindowsPrinter) Print(pdf []byte, printerName string) error {
	tmp, err := os.CreateTemp("", "arghzprint-*.pdf")
	if err != nil {
		return fmt.Errorf("create temp pdf: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(pdf); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp pdf: %w", err)
	}
	tmp.Close()

	var stderr bytes.Buffer
	cmd := exec.Command(p.sumatraBin,
		"-print-to", printerName,
		"-silent",
		"-exit-when-done",
		tmp.Name(),
	)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sumatra print: %w — %s", err, stderr.String())
	}

	if stderr.Len() > 0 {
		return fmt.Errorf("sumatra: %s", stderr.String())
	}

	return nil
}

func (p *WindowsPrinter) ListPrinters() ([]string, error) {
	// wmic is deprecated in newer Windows but still works; PowerShell is the modern alternative
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-Printer | Select-Object -ExpandProperty Name",
	).Output()
	if err != nil {
		// fall back to wmic if powershell fails
		return listViaWmic()
	}

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func listViaWmic() ([]string, error) {
	out, err := exec.Command("wmic", "printer", "get", "name").Output()
	if err != nil {
		return nil, fmt.Errorf("list printers: %w", err)
	}

	var names []string
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 {
			continue // header "Name"
		}
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// SumatraBinName is the expected filename for the bundled SumatraPDF binary.
const SumatraBinName = "SumatraPDF.exe"

// FindOrExtract resolves the SumatraPDF binary, extracting the bundled copy
// to dataDir on first run if embedded data is provided.
func FindOrExtract(dataDir string, embedded []byte) (string, error) {
	extracted := filepath.Join(dataDir, "tools", SumatraBinName)

	if len(embedded) > 0 {
		if err := extractIfNeeded(extracted, embedded); err != nil {
			return "", err
		}
		return extracted, nil
	}

	path, err := exec.LookPath(SumatraBinName)
	if err != nil {
		return "", fmt.Errorf("SumatraPDF not found and no bundled binary: %w", err)
	}
	return path, nil
}

func extractIfNeeded(dst string, data []byte) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create tools dir: %w", err)
	}
	return os.WriteFile(dst, data, 0755)
}
