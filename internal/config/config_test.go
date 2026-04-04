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

// ---------------------------------------------------------------------------
// Webhook config validation tests
// ---------------------------------------------------------------------------

func TestValidate_Webhook_ValidURL(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Settings: config.Settings{
			Webhooks: []config.WebhookConfig{
				{URL: "https://hooks.example.com/abc", On: []string{"error"}},
			},
		},
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_Webhook_EmptyURL_Fails(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Settings: config.Settings{
			Webhooks: []config.WebhookConfig{
				{URL: "", On: []string{"error"}},
			},
		},
	}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for empty webhook URL")
	}
}

func TestValidate_Webhook_InvalidURL_Fails(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Settings: config.Settings{
			Webhooks: []config.WebhookConfig{
				{URL: "not-a-url", On: []string{"error"}},
			},
		},
	}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestValidate_Webhook_EmptyOn_DefaultsAll(t *testing.T) {
	cfg := &config.KingdomConfig{
		Name: "test",
		Settings: config.Settings{
			Webhooks: []config.WebhookConfig{
				{URL: "https://example.com/hook"},
			},
		},
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DefaultConfig / DefaultSettings
// ---------------------------------------------------------------------------

func TestDefaultConfig_HasName(t *testing.T) {
	cfg := config.DefaultConfig("my-project")
	if cfg.Name != "my-project" {
		t.Errorf("expected Name=my-project, got %q", cfg.Name)
	}
	if cfg.Vassals == nil {
		t.Error("expected non-nil Vassals slice")
	}
	if len(cfg.Patterns) == 0 {
		t.Error("expected default patterns to be non-empty")
	}
}

func TestDefaultConfig_PatternHasRegex(t *testing.T) {
	cfg := config.DefaultConfig("k")
	for _, p := range cfg.Patterns {
		if p.Regex == "" {
			t.Errorf("pattern %q has empty regex", p.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// LoadVassalManifest
// ---------------------------------------------------------------------------

func TestLoadVassalManifest_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vassal.json")
	content := `{"name":"my-vassal","description":"does stuff","version":"1.0.0"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m, err := config.LoadVassalManifest(path)
	if err != nil {
		t.Fatalf("LoadVassalManifest: %v", err)
	}
	if m.Name != "my-vassal" {
		t.Errorf("expected Name=my-vassal, got %q", m.Name)
	}
}

func TestLoadVassalManifest_FileNotFound(t *testing.T) {
	_, err := config.LoadVassalManifest("/nonexistent/vassal.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadVassalManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vassal.json")
	_ = os.WriteFile(path, []byte("not-json{{{"), 0644)

	_, err := config.LoadVassalManifest(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// VassalConfig methods
// ---------------------------------------------------------------------------

func TestAutostartOrDefault_NilReturnsTrue(t *testing.T) {
	v := config.VassalConfig{}
	if !v.AutostartOrDefault() {
		t.Error("expected AutostartOrDefault to return true when nil")
	}
}

func TestAutostartOrDefault_FalseSet(t *testing.T) {
	f := false
	v := config.VassalConfig{Autostart: &f}
	if v.AutostartOrDefault() {
		t.Error("expected AutostartOrDefault to return false when set to false")
	}
}

func TestAutostartOrDefault_TrueSet(t *testing.T) {
	tr := true
	v := config.VassalConfig{Autostart: &tr}
	if !v.AutostartOrDefault() {
		t.Error("expected AutostartOrDefault to return true when set to true")
	}
}

func TestTypeOrDefault_EmptyReturnsShell(t *testing.T) {
	v := config.VassalConfig{}
	if v.TypeOrDefault() != "shell" {
		t.Errorf("expected 'shell', got %q", v.TypeOrDefault())
	}
}

func TestTypeOrDefault_SetValue(t *testing.T) {
	v := config.VassalConfig{Type: "claude"}
	if v.TypeOrDefault() != "claude" {
		t.Errorf("expected 'claude', got %q", v.TypeOrDefault())
	}
}

func TestBaudRateOrDefault_ZeroReturns115200(t *testing.T) {
	v := config.VassalConfig{}
	if v.BaudRateOrDefault() != 115200 {
		t.Errorf("expected 115200, got %d", v.BaudRateOrDefault())
	}
}

func TestBaudRateOrDefault_SetValue(t *testing.T) {
	v := config.VassalConfig{BaudRate: 9600}
	if v.BaudRateOrDefault() != 9600 {
		t.Errorf("expected 9600, got %d", v.BaudRateOrDefault())
	}
}

// ---------------------------------------------------------------------------
// Validate — data_rate guard with min_bytes_per_sec
// ---------------------------------------------------------------------------

func TestValidate_DataRate_ValidKbps(t *testing.T) {
	v := baseVassal("stream")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "100kbps"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DataRate_ValidMbps(t *testing.T) {
	v := baseVassal("stream")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "1.5mbps"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DataRate_ValidBps(t *testing.T) {
	v := baseVassal("stream")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "500bps"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DataRate_InvalidMin(t *testing.T) {
	v := baseVassal("stream")
	v.Guards = []config.GuardConfig{{Type: "data_rate", Min: "notvalid"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for invalid min format")
	}
}

func TestValidate_DataRate_NoMin_RequiresMin(t *testing.T) {
	v := baseVassal("stream")
	v.Guards = []config.GuardConfig{{Type: "data_rate"}}
	cfg := &config.KingdomConfig{Name: "k", Vassals: []config.VassalConfig{v}}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("expected error for data_rate without min, got nil")
	}
}

// ---------------------------------------------------------------------------
// LoadConfig — missing and invalid YAML
// ---------------------------------------------------------------------------

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadConfig("/nonexistent/path/kingdom.yml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kingdom.yml")
	_ = os.WriteFile(path, []byte("{invalid: yaml: [[["), 0644)
	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kingdom.yml")
	yaml := "name: my-kingdom\nvassals:\n  - name: shell\n    command: /bin/sh\n"
	_ = os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Name != "my-kingdom" {
		t.Errorf("expected name=my-kingdom, got %q", cfg.Name)
	}
	if !strings.Contains(cfg.Vassals[0].Name, "shell") {
		t.Errorf("expected vassal named 'shell', got %q", cfg.Vassals[0].Name)
	}
}
