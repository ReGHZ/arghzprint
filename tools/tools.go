// Package tools provides embedded tool binaries extracted on first run.
// Actual binaries are gitignored — add them to tools/windows/ and tools/unix/
// before building. The embed directives reference paths that must exist at
// compile time; empty files are checked in as placeholders.
package tools

import (
	_ "embed"
	"runtime"
)

//go:embed windows/wkhtmltopdf.exe
var wkhtmltopdfWindows []byte

//go:embed windows/SumatraPDF.exe
var sumatraWindows []byte

//go:embed unix/wkhtmltopdf
var wkhtmltopdfUnix []byte

// WkhtmltopdfBin returns the embedded wkhtmltopdf binary for the current OS.
// Returns nil if no binary was embedded (build without bundled tools).
func WkhtmltopdfBin() []byte {
	if runtime.GOOS == "windows" {
		return zeroIfEmpty(wkhtmltopdfWindows)
	}
	return zeroIfEmpty(wkhtmltopdfUnix)
}

// SumatraBin returns the embedded SumatraPDF binary (Windows only).
func SumatraBin() []byte {
	if runtime.GOOS == "windows" {
		return zeroIfEmpty(sumatraWindows)
	}
	return nil
}

// zeroIfEmpty returns nil when the embedded file is a placeholder (< 1KB).
// Placeholder files check into git as empty or near-empty bytes.
func zeroIfEmpty(b []byte) []byte {
	if len(b) < 1024 {
		return nil
	}
	return b
}
