package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	kingDirName    = ".king"
	configFileName = "kingdom.yml"
)

// validSeverities defines the allowed severity levels for pattern configs.
var validSeverities = map[string]bool{
	"info":     true,
	"warning":  true,
	"error":    true,
	"critical": true,
}

// validGuardTypes defines the allowed guard type identifiers.
var validGuardTypes = map[string]bool{
	"port_check":   true,
	"log_watch":    true,
	"data_rate":    true,
	"health_check": true,
}

// parseMinBytesPerSec converts a human-readable rate string (e.g. "100bps",
// "1.5kbps", "2mbps") to a float64 bytes-per-second value.
func parseMinBytesPerSec(min string) (float64, error) {
	lower := strings.ToLower(strings.TrimSpace(min))
	switch {
	case strings.HasSuffix(lower, "mbps"):
		v, err := strconv.ParseFloat(strings.TrimSuffix(lower, "mbps"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid min value %q", min)
		}
		return v * 1024 * 1024, nil
	case strings.HasSuffix(lower, "kbps"):
		v, err := strconv.ParseFloat(strings.TrimSuffix(lower, "kbps"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid min value %q", min)
		}
		return v * 1024, nil
	case strings.HasSuffix(lower, "bps"):
		v, err := strconv.ParseFloat(strings.TrimSuffix(lower, "bps"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid min value %q", min)
		}
		return v, nil
	default:
		return 0, fmt.Errorf("invalid min format %q: must end in bps, kbps, or mbps", min)
	}
}

// LoadConfig loads kingdom config from the given path.
// If the file doesn't exist, returns an error.
func LoadConfig(path string) (*KingdomConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg KingdomConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Apply default settings for zero-value fields.
	defaults := DefaultSettings()
	if cfg.Settings.LogRetentionDays == 0 {
		cfg.Settings.LogRetentionDays = defaults.LogRetentionDays
	}
	if cfg.Settings.MaxOutputBuffer == "" {
		cfg.Settings.MaxOutputBuffer = defaults.MaxOutputBuffer
	}
	if cfg.Settings.EventCooldownSeconds == 0 {
		cfg.Settings.EventCooldownSeconds = defaults.EventCooldownSeconds
	}
	if cfg.Settings.AuditRetentionDays == 0 {
		cfg.Settings.AuditRetentionDays = defaults.AuditRetentionDays
	}
	if cfg.Settings.AuditIngestionRetentionDays == 0 {
		cfg.Settings.AuditIngestionRetentionDays = defaults.AuditIngestionRetentionDays
	}
	if cfg.Settings.SovereignApprovalTimeout == 0 {
		cfg.Settings.SovereignApprovalTimeout = defaults.SovereignApprovalTimeout
	}
	if cfg.Settings.AuditMaxTraceOutput == 0 {
		cfg.Settings.AuditMaxTraceOutput = defaults.AuditMaxTraceOutput
	}

	return &cfg, nil
}

// LoadOrCreateConfig loads config from .king/kingdom.yml in rootDir.
// If file doesn't exist, creates a default config and returns it.
func LoadOrCreateConfig(rootDir string) (*KingdomConfig, error) {
	configPath := filepath.Join(rootDir, kingDirName, configFileName)

	if _, err := os.Stat(configPath); err == nil {
		return LoadConfig(configPath)
	}

	// Config doesn't exist; create a default one.
	if err := EnsureKingDir(rootDir); err != nil {
		return nil, fmt.Errorf("creating .king directory: %w", err)
	}

	dirName := filepath.Base(rootDir)
	tmpl := "name: " + dirName + `
vassals: []
# Example vassal with guards:
# vassals:
#   - name: api
#     command: ./api-server
#     autostart: true
#     guards:
#       - type: port_check
#         port: 8080
#         expect: open
#         interval: 10
#         threshold: 3
#       - type: log_watch
#         fail_on:
#           - "CRITICAL"
#           - "panic:"
#         interval: 5
#       - type: data_rate
#         min: 100bps
#         interval: 10
#         threshold: 3
#       - type: health_check
#         exec: ./scripts/health.sh
#         timeout: 10
#         threshold: 3
patterns:
  - name: generic-error
    regex: '(?i)error|FAIL|panic:'
    severity: error
    summary_template: "Error detected in {vassal}: {match}"
settings:
  log_retention_days: 7
  max_output_buffer: 10MB
  event_cooldown_seconds: 30
  audit_retention_days: 7
  audit_ingestion_retention_days: 1
  sovereign_approval_timeout: 300
  audit_max_trace_output: 10000
`

	if err := os.WriteFile(configPath, []byte(tmpl), 0644); err != nil {
		return nil, fmt.Errorf("writing default config: %w", err)
	}

	return LoadConfig(configPath)
}

// Validate checks the config for consistency errors.
func Validate(cfg *KingdomConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("kingdom name must not be empty")
	}

	vassalNames := make(map[string]struct{}, len(cfg.Vassals))
	for i, v := range cfg.Vassals {
		if v.Name == "" {
			return fmt.Errorf("vassal at index %d: name must not be empty", i)
		}
		if v.Command == "" && v.Type != "serial" && v.Type != "claude" && v.Type != "codex" && v.Type != "gemini" {
			return fmt.Errorf("vassal %q: command must not be empty", v.Name)
		}
		if v.Type == "serial" {
			if v.SerialPort == "" {
				return fmt.Errorf("vassal %q: serial_port must not be empty for type:serial", v.Name)
			}
			if strings.HasPrefix(v.SerialPort, "auto:") {
				hint := strings.TrimPrefix(v.SerialPort, "auto:")
				validHints := map[string]bool{"esp32": true, "ftdi": true, "gps": true, "any": true}
				if !validHints[hint] {
					return fmt.Errorf("vassal %q: unknown serial auto-detect hint %q (valid: esp32, ftdi, gps, any)", v.Name, hint)
				}
			}
		}
		if _, exists := vassalNames[v.Name]; exists {
			return fmt.Errorf("duplicate vassal name: %q", v.Name)
		}
		vassalNames[v.Name] = struct{}{}

		// Validate guards
		for j, gc := range v.Guards {
			if !validGuardTypes[gc.Type] {
				return fmt.Errorf("vassal %q: guard at index %d: invalid type %q (must be port_check, log_watch, data_rate, or health_check)", v.Name, j, gc.Type)
			}
			interval := gc.Interval
			if interval == 0 {
				interval = 10
			}
			if interval < 1 {
				return fmt.Errorf("vassal %q: guard at index %d: interval must be >= 1 second", v.Name, j)
			}
			threshold := gc.Threshold
			if threshold == 0 {
				threshold = 3
			}
			if threshold < 1 {
				return fmt.Errorf("vassal %q: guard at index %d: threshold must be >= 1", v.Name, j)
			}
			switch gc.Type {
			case "port_check":
				if gc.Port < 1 || gc.Port > 65535 {
					return fmt.Errorf("vassal %q: guard at index %d (port_check): port must be 1–65535", v.Name, j)
				}
				if gc.Expect != "" && gc.Expect != "open" && gc.Expect != "closed" {
					return fmt.Errorf("vassal %q: guard at index %d (port_check): expect must be 'open' or 'closed'", v.Name, j)
				}
			case "log_watch":
				if len(gc.FailOn) == 0 {
					return fmt.Errorf("vassal %q: guard at index %d (log_watch): fail_on must not be empty", v.Name, j)
				}
				patterns := make([]*regexp.Regexp, 0, len(gc.FailOn))
				for _, pattern := range gc.FailOn {
					re, err := regexp.Compile(pattern)
					if err != nil {
						return fmt.Errorf("vassal %q: guard at index %d (log_watch): invalid fail_on pattern %q: %w", v.Name, j, pattern, err)
					}
					patterns = append(patterns, re)
				}
				cfg.Vassals[i].Guards[j].CompiledPatterns = patterns
			case "data_rate":
				if gc.Min == "" {
					return fmt.Errorf("vassal %q: guard at index %d (data_rate): min must not be empty", v.Name, j)
				}
				minRate, err := parseMinBytesPerSec(gc.Min)
				if err != nil {
					return fmt.Errorf("vassal %q: guard at index %d (data_rate): %w", v.Name, j, err)
				}
				cfg.Vassals[i].Guards[j].MinBytesPerSec = minRate
			case "health_check":
				if gc.Exec == "" {
					return fmt.Errorf("vassal %q: guard at index %d (health_check): exec must not be empty", v.Name, j)
				}
				if gc.Timeout != 0 && gc.Timeout < 1 {
					return fmt.Errorf("vassal %q: guard at index %d (health_check): timeout must be >= 1", v.Name, j)
				}
			}
		}
	}

	for i, p := range cfg.Patterns {
		if p.Severity != "" && !validSeverities[p.Severity] {
			return fmt.Errorf("pattern at index %d (%q): invalid severity %q (must be info, warning, error, or critical)", i, p.Name, p.Severity)
		}
		if p.Regex != "" {
			if _, err := regexp.Compile(p.Regex); err != nil {
				return fmt.Errorf("pattern at index %d (%q): invalid regex: %w", i, p.Name, err)
			}
		}
	}

	for i, wh := range cfg.Settings.Webhooks {
		if wh.URL == "" {
			return fmt.Errorf("webhook[%d]: url must not be empty", i)
		}
		if !strings.HasPrefix(wh.URL, "http://") && !strings.HasPrefix(wh.URL, "https://") {
			return fmt.Errorf("webhook[%d]: url must start with http:// or https://", i)
		}
	}

	return nil
}

// DefaultConfig creates a minimal default config for the given directory name.
func DefaultConfig(dirName string) *KingdomConfig {
	return &KingdomConfig{
		Name:    dirName,
		Vassals: []VassalConfig{},
		Patterns: []PatternConfig{
			{
				Name:            "generic-error",
				Regex:           `(?i)error|FAIL|panic:`,
				Severity:        "error",
				SummaryTemplate: "Error detected in {vassal}: {match}",
			},
		},
		Settings: DefaultSettings(),
	}
}

// EnsureKingDir creates the .king directory structure if it doesn't exist.
func EnsureKingDir(rootDir string) error {
	kingDir := filepath.Join(rootDir, kingDirName)
	return os.MkdirAll(kingDir, 0755)
}

// LoadVassalManifest loads a vassal.json from the given path.
func LoadVassalManifest(path string) (*VassalManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading vassal manifest: %w", err)
	}

	var manifest VassalManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing vassal manifest: %w", err)
	}

	return &manifest, nil
}
