package daemon

import (
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

func TestExpandSerialCommand_Linux(t *testing.T) {
	v := config.VassalConfig{
		Name:       "gps",
		Type:       "serial",
		SerialPort: "/dev/ttyUSB0",
		BaudRate:   9600,
	}
	cmd := expandSerialCommand(v)
	if cmd == "" {
		t.Fatal("expected non-empty command")
	}
	if !strings.Contains(cmd, "/dev/ttyUSB0") {
		t.Errorf("expected command to contain port, got: %q", cmd)
	}
	if !strings.Contains(cmd, "9600") {
		t.Errorf("expected command to contain baud rate, got: %q", cmd)
	}
}

func TestExpandSerialCommand_DefaultBaud(t *testing.T) {
	v := config.VassalConfig{
		Name:       "esp",
		Type:       "serial",
		SerialPort: "/dev/ttyUSB1",
		BaudRate:   0, // unset → default 115200
	}
	cmd := expandSerialCommand(v)
	if cmd == "" {
		t.Fatal("expected non-empty command")
	}
	if !strings.Contains(cmd, "115200") {
		t.Errorf("expected command to contain default baud 115200, got: %q", cmd)
	}
}

func TestExpandSerialCommand_NonSerial_Unchanged(t *testing.T) {
	v := config.VassalConfig{
		Name:    "build",
		Command: "make build",
	}
	cmd := expandSerialCommand(v)
	if cmd != "make build" {
		t.Fatalf("expected original command unchanged, got %q", cmd)
	}
}
