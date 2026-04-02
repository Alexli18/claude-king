// internal/fingerprint/contracts_test.go
package fingerprint_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/fingerprint"
)

func TestDefaultContracts_Go(t *testing.T) {
	dir := t.TempDir()
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeGo, dir)
	if len(contracts) == 0 {
		t.Fatal("expected at least one contract for Go projects")
	}
	found := false
	for _, c := range contracts {
		if c.Name == "go-vet-error" && c.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected go-vet-error contract, got: %+v", contracts)
	}
}

func TestDefaultContracts_NodeWithTest(t *testing.T) {
	dir := t.TempDir()
	// Write a package.json with a test script.
	pkg := map[string]any{
		"scripts": map[string]any{"test": "jest"},
	}
	data, _ := json.Marshal(pkg)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644)

	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeNode, dir)
	found := false
	for _, c := range contracts {
		if c.Name == "npm-test-failure" && c.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected npm-test-failure contract, got: %+v", contracts)
	}
}

func TestDefaultContracts_NodeWithEslint(t *testing.T) {
	dir := t.TempDir()
	pkg := map[string]any{
		"devDependencies": map[string]any{"eslint": "^8.0.0"},
	}
	data, _ := json.Marshal(pkg)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644)

	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeNode, dir)
	found := false
	for _, c := range contracts {
		if c.Name == "eslint-error" && c.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected eslint-error contract, got: %+v", contracts)
	}
}

func TestDefaultContracts_Unknown(t *testing.T) {
	dir := t.TempDir()
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeUnknown, dir)
	if len(contracts) != 0 {
		t.Errorf("expected 0 contracts for Unknown, got %d", len(contracts))
	}
}

func TestDefaultContracts_Hardware(t *testing.T) {
	dir := t.TempDir()
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeHardware, dir)
	if len(contracts) != 0 {
		t.Errorf("expected 0 contracts for Hardware, got %d", len(contracts))
	}
}

func TestDefaultContracts_ESP32(t *testing.T) {
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeESP32, "")
	if len(contracts) == 0 {
		t.Fatal("expected at least one ESP32 contract")
	}
	names := make(map[string]bool)
	for _, c := range contracts {
		names[c.Name] = true
	}
	required := []string{"esp32-panic", "esp32-abort", "esp32-brownout", "esp32-stackoverflow", "esp32-error", "esp32-warning"}
	for _, r := range required {
		if !names[r] {
			t.Errorf("missing required contract: %s", r)
		}
	}
}

func TestDefaultContracts_NMEA(t *testing.T) {
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeNMEA, "")
	if len(contracts) == 0 {
		t.Fatal("expected at least one NMEA contract")
	}
	names := make(map[string]bool)
	for _, c := range contracts {
		names[c.Name] = true
	}
	for _, r := range []string{"nmea-invalid-fix", "nmea-no-signal"} {
		if !names[r] {
			t.Errorf("missing required contract: %s", r)
		}
	}
}

func TestDefaultContracts_AT(t *testing.T) {
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeAT, "")
	if len(contracts) == 0 {
		t.Fatal("expected at least one AT contract")
	}
	names := make(map[string]bool)
	for _, c := range contracts {
		names[c.Name] = true
	}
	for _, r := range []string{"at-error", "at-no-carrier", "at-registration-denied"} {
		if !names[r] {
			t.Errorf("missing required contract: %s", r)
		}
	}
}

func TestDefaultContracts_ESP32_SeverityValues(t *testing.T) {
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeESP32, "")
	for _, c := range contracts {
		switch c.Severity {
		case "critical", "error", "warning", "info":
			// valid
		default:
			t.Errorf("contract %q has invalid severity %q", c.Name, c.Severity)
		}
	}
}
