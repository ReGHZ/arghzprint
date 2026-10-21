package printer

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
)

var ErrUnknownPrinter = errors.New("unknown network printer")

// Network prints PNG pages to raw-TCP ESC/POS printers looked up by name.
// Used on Android, where there's no OS spooler to hand a PDF to.
type Network struct {
	lookup func(name string) (addr string, ok bool)
	names  func() []string
}

func NewNetwork(lookup func(name string) (string, bool), names func() []string) *Network {
	return &Network{lookup: lookup, names: names}
}

func (n *Network) Print(page []byte, printerName string) error {
	addr, ok := n.lookup(printerName)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownPrinter, printerName)
	}

	img, err := png.Decode(bytes.NewReader(page))
	if err != nil {
		return fmt.Errorf("decode page png: %w", err)
	}

	return SendTCP(addr, EncodeRaster(img))
}

func (n *Network) ListPrinters() ([]string, error) {
	return n.names(), nil
}
