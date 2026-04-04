//go:build !windows

package discovery_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/discovery"
)

// TestFindSerialPort_LinuxVIDPID creates a fake /sys/class/tty structure
// and verifies FindSerialPortInRoot finds the ESP32 device by VID:PID.
func TestFindSerialPort_LinuxVIDPID(t *testing.T) {
	root := t.TempDir()

	// Build fake sysfs: root/sys/class/tty/ttyUSB0/device/idVendor
	ttyDevDir := filepath.Join(root, "sys", "class", "tty", "ttyUSB0", "device")
	if err := os.MkdirAll(ttyDevDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ttyDevDir, "idVendor"), []byte("10c4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ttyDevDir, "idProduct"), []byte("ea60\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create fake /dev entry
	devDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(devDir, 0755); err != nil {
		t.Fatal(err)
	}
	devPath := filepath.Join(devDir, "ttyUSB0")
	if err := os.WriteFile(devPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := discovery.FindSerialPortInRoot(root, "esp32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != devPath {
		t.Errorf("got %q, want %q", got, devPath)
	}
}

func TestFindSerialPort_Any(t *testing.T) {
	root := t.TempDir()

	// ttyACM0 — no idVendor/idProduct files, should still match "any"
	ttyDevDir := filepath.Join(root, "sys", "class", "tty", "ttyACM0", "device")
	if err := os.MkdirAll(ttyDevDir, 0755); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(devDir, 0755); err != nil {
		t.Fatal(err)
	}
	devPath := filepath.Join(devDir, "ttyACM0")
	if err := os.WriteFile(devPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := discovery.FindSerialPortInRoot(root, "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != devPath {
		t.Errorf("got %q, want %q", got, devPath)
	}
}

func TestFindSerialPort_NotFound(t *testing.T) {
	root := t.TempDir()
	_, err := discovery.FindSerialPortInRoot(root, "esp32")
	if !errors.Is(err, discovery.ErrNoSerialDevice) {
		t.Fatalf("expected ErrNoSerialDevice, got: %v", err)
	}
}

func TestFindSerialPort_WrongVIDPID(t *testing.T) {
	root := t.TempDir()

	// Device present but VID:PID doesn't match "esp32"
	ttyDevDir := filepath.Join(root, "sys", "class", "tty", "ttyUSB0", "device")
	if err := os.MkdirAll(ttyDevDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ttyDevDir, "idVendor"), []byte("1234\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ttyDevDir, "idProduct"), []byte("5678\n"), 0644); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(devDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "ttyUSB0"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := discovery.FindSerialPortInRoot(root, "esp32")
	if !errors.Is(err, discovery.ErrNoSerialDevice) {
		t.Fatalf("expected ErrNoSerialDevice for wrong VID:PID, got: %v", err)
	}
}

// TestFindSerialPort_OS exercises the OS-dispatch path (FindSerialPort).
// On macOS it calls findSerialMacOS; on Linux it calls FindSerialPortInRoot("/", ...).
// Either way, the test just verifies it returns a path or ErrNoSerialDevice.
func TestFindSerialPort_OSDispatch(t *testing.T) {
	_, err := discovery.FindSerialPort("any")
	if err != nil && !errors.Is(err, discovery.ErrNoSerialDevice) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindSerialPort_OSDispatch_KnownHints(t *testing.T) {
	hints := []string{"esp32", "ftdi", "gps", "any"}
	for _, hint := range hints {
		_, err := discovery.FindSerialPort(hint)
		if err != nil && !errors.Is(err, discovery.ErrNoSerialDevice) {
			t.Fatalf("FindSerialPort(%q): unexpected error: %v", hint, err)
		}
	}
}
