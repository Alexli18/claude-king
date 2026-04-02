package config_test

import (
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

func TestVassalConfig_SerialDefaults(t *testing.T) {
	v := config.VassalConfig{Type: "serial", SerialPort: "/dev/ttyUSB0"}
	if v.BaudRate != 0 {
		t.Fatalf("expected zero BaudRate, got %d", v.BaudRate)
	}
	if v.SerialProtocol != "" {
		t.Fatalf("expected empty SerialProtocol, got %q", v.SerialProtocol)
	}
}

func TestSettings_SecurityScannerDefaults(t *testing.T) {
	s := config.Settings{}
	if s.SecurityScanner != "" {
		t.Fatalf("expected empty SecurityScanner, got %q", s.SecurityScanner)
	}
	if s.SecurityScannerArgs != nil {
		t.Fatalf("expected nil SecurityScannerArgs")
	}
}
