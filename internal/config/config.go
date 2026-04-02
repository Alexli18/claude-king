package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
	cfg := DefaultConfig(dirName)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshalling default config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return nil, fmt.Errorf("writing default config: %w", err)
	}

	return cfg, nil
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
		if v.Command == "" {
			return fmt.Errorf("vassal %q: command must not be empty", v.Name)
		}
		if _, exists := vassalNames[v.Name]; exists {
			return fmt.Errorf("duplicate vassal name: %q", v.Name)
		}
		vassalNames[v.Name] = struct{}{}
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

	return nil
}

// DefaultConfig creates a minimal default config for the given directory name.
func DefaultConfig(dirName string) *KingdomConfig {
	autostart := true
	return &KingdomConfig{
		Name: dirName,
		Vassals: []VassalConfig{
			{
				Name:      "shell",
				Command:   "$SHELL",
				Autostart: &autostart,
			},
		},
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
