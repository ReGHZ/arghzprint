package printer

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"testing"
	"time"
)

func grayImage(w, h int, black func(x, y int) bool) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if black(x, y) {
				img.SetGray(x, y, color.Gray{0})
			} else {
				img.SetGray(x, y, color.Gray{255})
			}
		}
	}
	return img
}

func rasterHeader(rowBytes, rows int) []byte {
	return []byte{0x1d, 0x76, 0x30, 0x00, byte(rowBytes), byte(rowBytes >> 8), byte(rows), byte(rows >> 8)}
}

func job(parts ...[]byte) []byte {
	out := []byte{0x1b, 0x40}
	for _, p := range parts {
		out = append(out, p...)
	}
	return append(out, 0x1b, 0x64, 0x04, 0x1d, 0x56, 0x42, 0x00)
}

func TestEncodeRasterLayout(t *testing.T) {
	cases := []struct {
		name string
		img  image.Image
		want []byte
	}{
		{
			name: "8x2 full row then half row",
			img:  grayImage(8, 2, func(x, y int) bool { return y == 0 || x < 4 }),
			want: job(rasterHeader(1, 2), []byte{0xff, 0xf0}),
		},
		{
			name: "width 10 pads to 2 bytes",
			img:  grayImage(10, 1, func(x, y int) bool { return true }),
			want: job(rasterHeader(2, 1), []byte{0xff, 0xc0}),
		},
		{
			name: "all white",
			img:  grayImage(8, 1, func(x, y int) bool { return false }),
			want: job(rasterHeader(1, 1), []byte{0x00}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EncodeRaster(tc.img)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("got  % x\nwant % x", got, tc.want)
			}
		})
	}
}

func TestEncodeRasterChunks(t *testing.T) {
	data := EncodeRaster(grayImage(8, 300, func(x, y int) bool { return false }))

	first := data[2:10]
	if !bytes.Equal(first, rasterHeader(1, 256)) {
		t.Fatalf("first header % x", first)
	}
	secondAt := 2 + 8 + 256
	second := data[secondAt : secondAt+8]
	if !bytes.Equal(second, rasterHeader(1, 44)) {
		t.Fatalf("second header % x", second)
	}
}

func TestThreshold(t *testing.T) {
	cases := []struct {
		name string
		c    color.Color
		want bool
	}{
		{"black", color.Gray{0}, true},
		{"gray 127", color.Gray{127}, true},
		{"gray 128", color.Gray{128}, false},
		{"white", color.Gray{255}, false},
		{"transparent", color.NRGBA{0, 0, 0, 0}, false},
		{"red is dark enough", color.RGBA{255, 0, 0, 255}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBlack(tc.c); got != tc.want {
				t.Errorf("isBlack = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	src := grayImage(576, 700, func(x, y int) bool { return (x*y)%7 == 0 })

	got, err := DecodeRaster(EncodeRaster(src))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 576 || got.Bounds().Dy() != 700 {
		t.Fatalf("bounds %v", got.Bounds())
	}
	for y := 0; y < 700; y++ {
		for x := 0; x < 576; x++ {
			if (got.GrayAt(x, y).Y == 0) != isBlack(src.At(x, y)) {
				t.Fatalf("pixel %d,%d differs", x, y)
			}
		}
	}
}

func TestDecodeRasterRejectsGarbage(t *testing.T) {
	if _, err := DecodeRaster([]byte("hello")); !errors.Is(err, ErrNoRaster) {
		t.Errorf("err = %v, want ErrNoRaster", err)
	}
}

// listenPrinter is a real TCP endpoint standing in for a LAN printer
func listenPrinter(t *testing.T) (string, <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	got := make(chan []byte, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			data, _ := io.ReadAll(conn)
			conn.Close()
			got <- data
		}
	}()
	return ln.Addr().String(), got
}

func deadAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func receive(t *testing.T, got <-chan []byte) []byte {
	t.Helper()
	select {
	case d := <-got:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("printer received nothing")
		return nil
	}
}

func TestSendTCP(t *testing.T) {
	addr, got := listenPrinter(t)
	payload := []byte{0x1b, 0x40, 1, 2, 3}

	if err := SendTCP(addr, payload); err != nil {
		t.Fatal(err)
	}
	if d := receive(t, got); !bytes.Equal(d, payload) {
		t.Errorf("received % x", d)
	}
}

func TestSendTCPRefused(t *testing.T) {
	if err := SendTCP(deadAddr(t), []byte{1}); err == nil {
		t.Error("expected dial error")
	}
}

func TestNetworkPrint(t *testing.T) {
	addr, got := listenPrinter(t)
	n := NewNetwork(
		func(name string) (string, bool) { return addr, name == "Dapur" },
		func() []string { return []string{"Dapur"} },
	)

	img := grayImage(8, 1, func(x, y int) bool { return true })
	var page bytes.Buffer
	if err := png.Encode(&page, img); err != nil {
		t.Fatal(err)
	}

	if err := n.Print(page.Bytes(), "Dapur"); err != nil {
		t.Fatal(err)
	}
	if d := receive(t, got); !bytes.Equal(d, EncodeRaster(img)) {
		t.Errorf("received % x", d)
	}

	if err := n.Print(page.Bytes(), "Bar"); !errors.Is(err, ErrUnknownPrinter) {
		t.Errorf("err = %v, want ErrUnknownPrinter", err)
	}

	names, _ := n.ListPrinters()
	if len(names) != 1 || names[0] != "Dapur" {
		t.Errorf("ListPrinters = %v", names)
	}
}
