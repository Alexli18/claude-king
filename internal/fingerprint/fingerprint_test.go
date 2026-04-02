// internal/fingerprint/fingerprint_test.go
package fingerprint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/fingerprint"
)

func mkFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprint_Go(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "go.mod")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeGo {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeGo)
	}
}

func TestFingerprint_Node(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "package.json")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeNode {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeNode)
	}
}

func TestFingerprint_GoTakesPriorityOverNode(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "go.mod")
	mkFile(t, dir, "package.json")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeGo {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeGo)
	}
}

func TestFingerprint_Hardware_Ino(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "sketch.ino")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeHardware {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeHardware)
	}
}

func TestFingerprint_Hardware_CMakefile(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "main.c")
	mkFile(t, dir, "Makefile")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeHardware {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeHardware)
	}
}

func TestFingerprint_Unknown(t *testing.T) {
	dir := t.TempDir()
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeUnknown {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeUnknown)
	}
}

func TestSerialProtocolForBaud(t *testing.T) {
	cases := []struct {
		baud     int
		expected fingerprint.ProjectType
	}{
		{4800, fingerprint.ProjectTypeNMEA},
		{9600, fingerprint.ProjectTypeNMEA},
		{19200, fingerprint.ProjectTypeNMEA},
		{38400, fingerprint.ProjectTypeNMEA},
		{115200, fingerprint.ProjectTypeESP32},
		{57600, fingerprint.ProjectTypeUnknown},
		{0, fingerprint.ProjectTypeESP32}, // default baud → ESP32
	}
	for _, tc := range cases {
		got := fingerprint.SerialProtocolForBaud(tc.baud)
		if got != tc.expected {
			t.Errorf("baud %d: expected %q, got %q", tc.baud, tc.expected, got)
		}
	}
}

func TestProjectType_SerialConstants(t *testing.T) {
	if fingerprint.ProjectTypeESP32 == "" {
		t.Fatal("ProjectTypeESP32 must not be empty")
	}
	if fingerprint.ProjectTypeNMEA == "" {
		t.Fatal("ProjectTypeNMEA must not be empty")
	}
	if fingerprint.ProjectTypeAT == "" {
		t.Fatal("ProjectTypeAT must not be empty")
	}
	// Verify distinct values
	constants := []fingerprint.ProjectType{
		fingerprint.ProjectTypeESP32,
		fingerprint.ProjectTypeNMEA,
		fingerprint.ProjectTypeAT,
	}
	seen := make(map[fingerprint.ProjectType]bool)
	for _, c := range constants {
		if seen[c] {
			t.Errorf("duplicate ProjectType constant value: %q", c)
		}
		seen[c] = true
	}
}
