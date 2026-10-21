// fakeprinter pretends to be a raw-TCP ESC/POS printer and saves every job as a PNG,
// so receipts from a phone/tablet can be checked without the real printer
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ReGHZ/arghzprint/internal/printer"
)

var cutSeq = []byte{0x1d, 0x56, 0x42, 0x00}

func main() {
	addr := flag.String("addr", ":9100", "listen address")
	out := flag.String("out", "fakeprints", "directory for received PNGs")
	flag.Parse()

	if err := os.MkdirAll(*out, 0755); err != nil {
		slog.Error("create output dir", "err", err)
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		slog.Error("listen", "addr", *addr, "err", err)
		os.Exit(1)
	}
	slog.Info("fakeprinter listening", "addr", *addr, "out", *out)

	var seq atomic.Int64
	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Warn("accept", "err", err)
			continue
		}
		go handle(conn, *out, seq.Add(1))
	}
}

func handle(conn net.Conn, out string, seq int64) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	data, err := io.ReadAll(conn)
	if err != nil && len(data) == 0 {
		slog.Warn("read job", "remote", conn.RemoteAddr(), "err", err)
		return
	}

	img, err := printer.DecodeRaster(data)
	if err != nil {
		slog.Warn("not a raster job", "bytes", len(data), "err", err)
		return
	}

	name := filepath.Join(out, fmt.Sprintf("%s-%03d.png", time.Now().Format("150405"), seq))
	f, err := os.Create(name)
	if err != nil {
		slog.Error("create png", "err", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		slog.Error("encode png", "err", err)
		return
	}

	slog.Info("job saved",
		"file", name,
		"width", img.Bounds().Dx(),
		"height", img.Bounds().Dy(),
		"cut", bytes.HasSuffix(data, cutSeq),
	)
}
