package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

// ---------------------------------------------------------------------------
// Guard config validation tests (T020)
// ---------------------------------------------------------------------------

func baseVassal(name string) config.VassalConfig {
	return config.VassalConfig{Name: name, Command: "echo test"}
}

func TestValidate_Guard_PortCheck_Valid(t *testing.T) {
	v := baseVassal("api")
	v.Guards = []config.GuardConfig{{Type: "port_check", Port: 8080}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_Guard_InvalidType(t *testing.T) {
	v := baseVassal("api")
	v.Guards = []config.GuardConfig{{Type: "bogus"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid guard type")
	}
}

func TestValidate_Guard_PortCheck_BadPort(t *testing.T) {
	v := baseVassal("api")
	v.Guards = []config.GuardConfig{{Type: "port_check", Port: 0}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for port=0")
	}
}

func TestValidate_Guard_PortCheck_BadExpect(t *testing.T) {
	v := baseVassal("api")
	v.Guards = []config.GuardConfig{{Type: "port_check", Port: 80, Expect: "maybe"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid expect value")
	}
}

func TestValidate_Guard_LogWatch_Valid(t *testing.T) {
	v := baseVassal("worker")
	v.Guards = []config.GuardConfig{{Type: "log_watch", FailOn: []string{"ERROR", "panic"}}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Compiled patterns must be populated.
	if len(cfg.Vassals[0].Guards[0].CompiledPatterns) != 2 {
		t.Fatalf("expected 2 compiled patterns, got %d", len(cfg.Vassals[0].Guards[0].CompiledPatterns))
	}
}

func TestValidate_Guard_LogWatch_EmptyFailOn(t *testing.T) {
	v := baseVassal("worker")
	v.Guards = []config.GuardConfig{{Type: "log_watch", FailOn: []string{}}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for empty fail_on")
	}
}

func TestValidate_Guard_LogWatch_BadRegex(t *testing.T) {
	v := baseVassal("worker")
	v.Guards = []config.GuardConfig{{Type: "log_watch", FailOn: []string{"[invalid"}}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestValidate_Guard_DataRate_Valid(t *testing.T) {
	v := baseVassal("sensor")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "1.5kbps"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := cfg.Vassals[0].Guards[0].MinBytesPerSec
	want := 1.5 * 1024.0
	if got != want {
		t.Fatalf("expected MinBytesPerSec=%.1f, got %.1f", want, got)
	}
}

func TestValidate_Guard_DataRate_MbpsUnit(t *testing.T) {
	v := baseVassal("sensor")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "2mbps"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 2.0 * 1024 * 1024
	got := cfg.Vassals[0].Guards[0].MinBytesPerSec
	if got != want {
		t.Fatalf("expected %.1f, got %.1f", want, got)
	}
}

func TestValidate_Guard_DataRate_BadMin(t *testing.T) {
	v := baseVassal("sensor")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "fast"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid min format")
	}
}

func TestValidate_Guard_DataRate_EmptyMin(t *testing.T) {
	v := baseVassal("sensor")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: ""}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for empty min")
	}
}

func TestValidate_Guard_HealthCheck_Valid(t *testing.T) {
	v := baseVassal("svc")
	v.Guards = []config.GuardConfig{{Type: "health_check", Exec: "./check.sh"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_Guard_HealthCheck_MissingExec(t *testing.T) {
	v := baseVassal("svc")
	v.Guards = []config.GuardConfig{{Type: "health_check", Exec: ""}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for empty exec")
	}
}

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

func TestValidateConfig_CodexGeminiVassals(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Vassals: []config.VassalConfig{
			{Name: "coder", Type: "codex"},
			{Name: "analyst", Type: "gemini", Model: "gemini-2.0-flash"},
		},
	}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("expected codex/gemini vassals to be valid, got: %v", err)
	}
}

func TestValidateConfig_CodexGemini_Specialization(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Vassals: []config.VassalConfig{
			{Name: "coder", Type: "codex", Specialization: "TypeScript, React"},
		},
	}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.Vassals[0].Specialization != "TypeScript, React" {
		t.Error("specialization not preserved")
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
