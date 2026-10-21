package printer

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"net"
	"time"
)

const (
	// cheap printers overflow their input buffer on one huge GS v 0 block
	rasterChunkRows = 256
	dialTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
)

var (
	escInit = []byte{0x1b, 0x40}
	escFeed = []byte{0x1b, 0x64, 0x04}
	gsCut   = []byte{0x1d, 0x56, 0x42, 0x00}
)

var ErrNoRaster = errors.New("no GS v 0 raster in data")

// EncodeRaster builds a full ESC/POS job: init, raster image, feed, partial cut
func EncodeRaster(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rowBytes := (w + 7) / 8

	var out bytes.Buffer
	out.Write(escInit)
	for y0 := 0; y0 < h; y0 += rasterChunkRows {
		rows := min(rasterChunkRows, h-y0)
		out.Write([]byte{0x1d, 0x76, 0x30, 0x00, byte(rowBytes), byte(rowBytes >> 8), byte(rows), byte(rows >> 8)})
		line := make([]byte, rowBytes)
		for y := y0; y < y0+rows; y++ {
			clear(line)
			for x := 0; x < w; x++ {
				if isBlack(img.At(b.Min.X+x, b.Min.Y+y)) {
					line[x/8] |= 0x80 >> (x % 8)
				}
			}
			out.Write(line)
		}
	}
	out.Write(escFeed)
	out.Write(gsCut)
	return out.Bytes()
}

// threshold, not dithering — receipts are solid black text and dithering blurs it
func isBlack(c color.Color) bool {
	r, g, b, a := c.RGBA()
	if a < 0x8000 {
		return false
	}
	return (299*r+587*g+114*b)/1000 < 0x8000
}

// DecodeRaster rebuilds the image from GS v 0 blocks. It only understands what
// EncodeRaster writes — enough for fakeprinter and tests.
func DecodeRaster(data []byte) (*image.Gray, error) {
	type block struct {
		rowBytes, rows int
		bits           []byte
	}

	var blocks []block
	for i := 0; i+8 <= len(data); {
		if data[i] != 0x1d || data[i+1] != 0x76 || data[i+2] != 0x30 {
			i++
			continue
		}
		rowBytes := int(data[i+4]) | int(data[i+5])<<8
		rows := int(data[i+6]) | int(data[i+7])<<8
		start, n := i+8, rowBytes*rows
		if start+n > len(data) {
			return nil, fmt.Errorf("truncated raster block at byte %d", i)
		}
		blocks = append(blocks, block{rowBytes, rows, data[start : start+n]})
		i = start + n
	}
	if len(blocks) == 0 {
		return nil, ErrNoRaster
	}

	w, h := blocks[0].rowBytes*8, 0
	for _, bl := range blocks {
		h += bl.rows
	}

	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	y := 0
	for _, bl := range blocks {
		for r := 0; r < bl.rows; r++ {
			for x := 0; x < min(bl.rowBytes*8, w); x++ {
				if bl.bits[r*bl.rowBytes+x/8]&(0x80>>(x%8)) != 0 {
					img.SetGray(x, y, color.Gray{0})
				}
			}
			y++
		}
	}
	return img, nil
}

// SendTCP writes one job and closes — cheap LAN printers misbehave when a
// connection is held open between jobs
func SendTCP(addr string, data []byte) error {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return fmt.Errorf("dial printer %s: %w", addr, err)
	}
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write printer %s: %w", addr, err)
	}
	return nil
}
