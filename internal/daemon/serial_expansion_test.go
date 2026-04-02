package daemon

import (
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
	want := "stty -F /dev/ttyUSB0 9600 raw -echo && cat /dev/ttyUSB0"
	got := expandSerialCommand(v)
	if got != want {
		t.Errorf("unexpected command:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExpandSerialCommand_DefaultBaud(t *testing.T) {
	v := config.VassalConfig{
		Name:       "esp",
		Type:       "serial",
		SerialPort: "/dev/ttyUSB1",
		BaudRate:   0, // unset → default 115200
	}
	want := "stty -F /dev/ttyUSB1 115200 raw -echo && cat /dev/ttyUSB1"
	got := expandSerialCommand(v)
	if got != want {
		t.Errorf("unexpected command:\n  got:  %q\n  want: %q", got, want)
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
