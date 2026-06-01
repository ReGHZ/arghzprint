// Package printer sends PDF bytes to an OS-managed printer by name.
// The interface is intentionally minimal — callers don't need to know
// whether they're on Windows or Unix.
package printer

// Printer sends a PDF to a named printer.
type Printer interface {
	Print(pdf []byte, printerName string) error

	// ListPrinters returns the names of all printers available on this machine.
	ListPrinters() ([]string, error)
}
