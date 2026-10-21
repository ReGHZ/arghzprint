// Package mobile is the gomobile entry point for the Android shell.
// Only gobind-friendly types cross this boundary: string, int, []byte, error, interfaces.
package mobile

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ReGHZ/arghzprint/internal/app"
)

// HTMLRenderer is implemented in Kotlin with an offscreen WebView.
type HTMLRenderer interface {
	RenderPNG(html string, cssWidthPx int, widthPx int) ([]byte, error)
}

var (
	mu     sync.Mutex
	cancel context.CancelFunc // nil once Stop was called
	done   chan struct{}      // closed when Run returns, for whatever reason
)

// Start runs the daemon in the background and returns. A call while it's running
// is a no-op — Android may recreate the service on a START_STICKY restart. If the
// previous Run died on its own (e.g. UI port taken), Start begins a fresh one.
func Start(dataDir string, r HTMLRenderer) error {
	mu.Lock()
	defer mu.Unlock()

	if done != nil {
		select {
		case <-done:
		default:
			if cancel != nil {
				return nil
			}
			// a timed-out Stop is still winding down; two Apps on one DB would
			// both resume the same unfinished jobs and print them twice
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				return errors.New("previous instance is still shutting down")
			}
		}
	}
	if dataDir == "" || r == nil {
		return errors.New("dataDir and renderer are required")
	}

	a, err := app.NewWithOptions(app.Options{DataDir: dataDir, PNG: r})
	if err != nil {
		return err
	}

	if cancel != nil {
		cancel()
	}
	ctx, c := context.WithCancel(context.Background())
	d := make(chan struct{})
	go func() {
		defer close(d)
		if err := a.Run(ctx); err != nil {
			slog.Error("run failed", "err", err)
		}
	}()

	cancel, done = c, d
	return nil
}

// Stop blocks until shutdown finishes or 10s pass
func Stop() {
	mu.Lock()
	defer mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	cancel = nil
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		slog.Warn("shutdown timed out")
	}
}

// Running is false after Stop and when Run died on its own — the shell polls it
// to restart a daemon that failed after Start returned
func Running() bool {
	mu.Lock()
	defer mu.Unlock()

	if done == nil || cancel == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}
