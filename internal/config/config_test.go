package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

func TestValidate_AcceptsAutoSerialPort(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Vassals: []config.VassalConfig{
			{Name: "fw", Type: "serial", SerialPort: "auto:esp32"},
		},
	}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("expected no error for auto:esp32, got: %v", err)
	}
}

func TestValidate_AcceptsAutoAny(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Vassals: []config.VassalConfig{
			{Name: "gps", Type: "serial", SerialPort: "auto:any"},
		},
	}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("expected no error for auto:any, got: %v", err)
	}
}

func TestValidate_RejectsUnknownAutoHint(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Vassals: []config.VassalConfig{
			{Name: "fw", Type: "serial", SerialPort: "auto:unknown"},
		},
	}
	if err := config.Validate(cfg); err == nil {
		t.Error("expected error for auto:unknown, got nil")
	}
}

func TestLoadOrCreateConfig_WritesEmptyVassals(t *testing.T) {
	rootDir := t.TempDir()

	cfg, err := config.LoadOrCreateConfig(rootDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Vassals) != 0 {
		t.Errorf("expected 0 vassals, got %d: %v", len(cfg.Vassals), cfg.Vassals)
	}

	// Check the written file contains "vassals: []" and a comment.
	data, err := os.ReadFile(filepath.Join(rootDir, ".king", "kingdom.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "vassals: []") {
		t.Errorf("expected 'vassals: []' in file, got:\n%s", content)
	}
	if !strings.Contains(content, "# Example") {
		t.Errorf("expected '# Example' comment in file, got:\n%s", content)
	}
}
