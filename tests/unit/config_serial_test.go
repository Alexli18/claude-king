package config_test

import (
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

func TestVassalConfig_SerialDefaults(t *testing.T) {
	v := config.VassalConfig{Type: "serial", SerialPort: "/dev/ttyUSB0"}
	// Raw field is 0 (unset)
	if v.BaudRate != 0 {
		t.Fatalf("expected zero BaudRate raw field, got %d", v.BaudRate)
	}
	// BaudRateOrDefault returns 115200
	if v.BaudRateOrDefault() != 115200 {
		t.Fatalf("expected BaudRateOrDefault()=115200, got %d", v.BaudRateOrDefault())
	}
	// Explicit baud rate is preserved
	v2 := config.VassalConfig{BaudRate: 9600}
	if v2.BaudRateOrDefault() != 9600 {
		t.Fatalf("expected BaudRateOrDefault()=9600, got %d", v2.BaudRateOrDefault())
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
