//go:build !windows

package printer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// UnixPrinter sends PDFs via lp (CUPS).
type UnixPrinter struct{}

func New(_ string) Printer {
	return &UnixPrinter{}
}

func (p *UnixPrinter) Print(pdf []byte, printerName string) error {
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
	cmd := exec.Command("lp", "-d", printerName, tmp.Name())
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lp: %w — %s", err, stderr.String())
	}

	return nil
}

func (p *UnixPrinter) ListPrinters() ([]string, error) {
	out, err := exec.Command("lpstat", "-a").Output()
	if err != nil {
		// lpstat -a fails when CUPS has no printers configured
		return nil, nil
	}

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// lpstat -a output: "PrinterName accepting requests since ..."
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names, nil
}

// FindOrExtract on Unix — SumatraPDF is Windows only, no-op here.
func FindOrExtract(_ string, _ []byte) (string, error) {
	return "", nil
}
