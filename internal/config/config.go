package config

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Config is the runtime configuration for arghzprint.
// Stored as config.json in the OS-appropriate data directory.
type Config struct {
	// AgentID is a stable UUID identifying this daemon instance to the backend.
	// Generated once on first run and persisted — never regenerated, so the
	// backend can attribute job claims and status updates to a specific agent.
	AgentID string `json:"agent_id"`

	// BackendURL is the base URL of the backend (e.g. "https://api.example.com").
	BackendURL string `json:"backend_url"`

	// PrinterToken is the bearer token used for all backend API calls.
	PrinterToken string `json:"printer_token"`

	// WSPath is the WebSocket endpoint path on the backend.
	WSPath string `json:"ws_path"`

	// PollingIntervalSeconds is used when WebSocket is unavailable.
	PollingIntervalSeconds int `json:"polling_interval_seconds"`

	// WebUIPort is the port for the local settings + template editor UI.
	WebUIPort int `json:"web_ui_port"`

	// PrinterMap maps job type strings to OS printer names.
	// e.g. { "KITCHEN": "Epson-TM-T82-Dapur", "CUSTOMER": "Epson-Kasir" }
	PrinterMap map[string]string `json:"printer_map"`

	// EnabledTypes is the allowlist of job types this agent prints.
	// A type must be present and set to true. Anything not listed (or false)
	// is left for another agent — never claimed or canceled here.
	// Also reported to the backend in printer.hello.
	EnabledTypes map[string]bool `json:"enabled_types"`

	// PriorityMap sets processing priority per job type. Higher = processed first.
	// Takes precedence over any priority value sent by the backend.
	// Types not listed fall back to whatever the backend sent (default 0 for wulfcafe).
	PriorityMap map[string]int `json:"priority_map"`

	// ConnectionMode is "websocket" (default) or "polling".
	// Use "polling" for backends that don't expose a WebSocket endpoint.
	ConnectionMode string `json:"connection_mode"`

	// MaxRetries is how many times the print worker retries a failed job
	// before marking it FAILED and moving on.
	MaxRetries int `json:"max_retries"`

	// PrintWidthMM is the actual printable width of the paper, in mm —
	// not the nominal roll width. Thermal printer drivers often report a
	// printable area narrower than the roll itself (e.g. Windows shows
	// "Printer Paper(80(72) x 210mm)" for what's sold as "80mm paper" —
	// only 72mm of that is printable). Content rendered wider than this
	// gets clipped by the driver. Check the driver's Paper Size in
	// Printing Preferences -> Advanced to find the real number.
	PrintWidthMM int `json:"print_width_mm"`
}

func defaults() Config {
	return Config{
		BackendURL:             "http://localhost:3001",
		PrinterToken:           "",
		WSPath:                 "/api/printer/ws",
		PollingIntervalSeconds: 5,
		WebUIPort:              7878,
		PrinterMap:             map[string]string{},
		EnabledTypes:           map[string]bool{},
		PriorityMap: map[string]int{
			"KITCHEN": 2,
			"BAR":     1,
		},
		ConnectionMode: "websocket",
		MaxRetries:     3,
		PrintWidthMM:   80,
	}
}

// Manager handles loading and saving the config file.
type Manager struct {
	mu      sync.RWMutex
	path    string
	current Config
}

// New loads the config from disk, creating it with defaults if it doesn't exist.
func New() (*Manager, error) {
	path, err := configPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	m := &Manager{path: path}

	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *Manager) Save(c Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.current = c
	return m.write()
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		m.current = defaults()
		m.current.AgentID = newAgentID()
		return m.write()
	}
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	cfg := defaults()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	m.current = cfg

	// configs written before agent_id existed — backfill and persist once
	if m.current.AgentID == "" {
		m.current.AgentID = newAgentID()
		return m.write()
	}
	return nil
}

// newAgentID returns a random UUIDv4 string.
func newAgentID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (m *Manager) write() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(m.current, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(m.path, data, 0644)
}

// configPath returns the OS-appropriate path for config.json.
func configPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// DataDir returns the base directory for all arghzprint data (config, templates, extracted tools).
func DataDir() (string, error) {
	return dataDir()
}

func dataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			return "", fmt.Errorf("%%APPDATA%% not set")
		}
		return filepath.Join(base, "arghzprint"), nil

	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "arghzprint"), nil

	default: // linux and others
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "arghzprint"), nil
	}
}
