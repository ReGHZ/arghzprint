package mobile

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type noRender struct{}

func (noRender) RenderPNG(string, int, int) ([]byte, error) { return nil, errors.New("unused") }

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// dataDir with its own UI port and an unreachable backend, so tests never touch 7878
func dataDir(t *testing.T, port int) string {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`{"agent_id":"test","backend_url":"http://127.0.0.1:1","web_ui_port":%d}`, port)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func waitRunning(t *testing.T, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if Running() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Running() never became %v", want)
}

func TestStartStopRestart(t *testing.T) {
	dir := dataDir(t, freePort(t))

	if err := Start(dir, noRender{}); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, true)
	if err := Start(dir, noRender{}); err != nil {
		t.Fatalf("second Start while running: %v", err)
	}

	Stop()
	if Running() {
		t.Fatal("still running after Stop")
	}

	if err := Start(dir, noRender{}); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, true)
	Stop()
}

// a Run that dies early (UI port taken) must not leave a zombie that makes every
// later Start a silent no-op
func TestStartRecoversAfterRunFailed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	dir := dataDir(t, port)

	if err := Start(dir, noRender{}); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, false)

	ln.Close()
	if err := Start(dir, noRender{}); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, true)
	Stop()
}
