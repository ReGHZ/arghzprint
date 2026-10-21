package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAtCreatesDefaults(t *testing.T) {
	dir := t.TempDir()

	m, err := NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}

	c := m.Get()
	if c.AgentID == "" {
		t.Error("agent_id not generated")
	}
	if c.MaxJobAgeMinutes != 30 {
		t.Errorf("max_job_age_minutes = %d, want 30", c.MaxJobAgeMinutes)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Errorf("config.json not written: %v", err)
	}
}

func TestNewAtOldConfigGetsNewDefaults(t *testing.T) {
	dir := t.TempDir()
	old := `{"agent_id":"abc","backend_url":"http://x","max_retries":3}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}

	c := m.Get()
	if c.AgentID != "abc" {
		t.Errorf("agent_id = %q, want abc", c.AgentID)
	}
	if c.MaxJobAgeMinutes != 30 {
		t.Errorf("max_job_age_minutes = %d, want 30", c.MaxJobAgeMinutes)
	}
	if len(c.NetworkPrinters) != 0 {
		t.Errorf("network_printers = %v, want empty", c.NetworkPrinters)
	}
	if c.PrintWidthMM != 80 {
		t.Errorf("print_width_mm = %d, want 80", c.PrintWidthMM)
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      []NetworkPrinter
		want    []NetworkPrinter
		wantErr bool
	}{
		{
			name: "defaults filled and trimmed",
			in:   []NetworkPrinter{{Name: " Dapur ", Host: " 192.168.1.50 "}},
			want: []NetworkPrinter{{Name: "Dapur", Host: "192.168.1.50", Port: 9100, WidthDots: 576}},
		},
		{
			name: "58mm printer",
			in:   []NetworkPrinter{{Name: "Kasir", Host: "10.0.0.9", Port: 9101, WidthDots: 384}},
			want: []NetworkPrinter{{Name: "Kasir", Host: "10.0.0.9", Port: 9101, WidthDots: 384}},
		},
		{name: "empty name", in: []NetworkPrinter{{Host: "1.2.3.4"}}, wantErr: true},
		{name: "duplicate name", in: []NetworkPrinter{{Name: "A", Host: "1.1.1.1"}, {Name: "A", Host: "2.2.2.2"}}, wantErr: true},
		{name: "empty host", in: []NetworkPrinter{{Name: "A"}}, wantErr: true},
		{name: "port out of range", in: []NetworkPrinter{{Name: "A", Host: "1.1.1.1", Port: 70000}}, wantErr: true},
		{name: "width not multiple of 8", in: []NetworkPrinter{{Name: "A", Host: "1.1.1.1", WidthDots: 500}}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{NetworkPrinters: tc.in}
			c.Normalize()
			err := c.Validate()

			if tc.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			for i, p := range c.NetworkPrinters {
				if p != tc.want[i] {
					t.Errorf("printer %d = %+v, want %+v", i, p, tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeFixesMaxJobAge(t *testing.T) {
	c := Config{MaxJobAgeMinutes: 0}
	c.Normalize()
	if c.MaxJobAgeMinutes != 30 {
		t.Errorf("max_job_age_minutes = %d, want 30", c.MaxJobAgeMinutes)
	}
}

func TestNetworkPrinterLookup(t *testing.T) {
	c := Config{NetworkPrinters: []NetworkPrinter{{Name: "Dapur", Host: "192.168.1.50", Port: 9100, WidthDots: 576}}}

	p, ok := c.NetworkPrinter("Dapur")
	if !ok || p.Addr() != "192.168.1.50:9100" {
		t.Errorf("lookup = %+v %v", p, ok)
	}
	if _, ok := c.NetworkPrinter("Bar"); ok {
		t.Error("unknown printer found")
	}
}
