package config

import "regexp"

// KingdomConfig represents the top-level kingdom configuration from .king/kingdom.yml
type KingdomConfig struct {
	Name         string          `yaml:"name"`
	Vassals      []VassalConfig  `yaml:"vassals"`
	Patterns     []PatternConfig `yaml:"patterns,omitempty"`
	ArtifactsDir string          `yaml:"artifacts_dir,omitempty"`
	Settings     Settings        `yaml:"settings,omitempty"`
}

// VassalConfig represents a vassal definition in the kingdom configuration
type VassalConfig struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Cwd           string            `yaml:"cwd,omitempty"`
	RepoPath      string            `yaml:"repo_path,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	Autostart     *bool             `yaml:"autostart,omitempty"`
	RestartPolicy string            `yaml:"restart_policy,omitempty"`
	Type          string            `yaml:"type,omitempty"`     // "shell" (default) | "claude"
	Host          string            `yaml:"host,omitempty"`     // SSH host for remote vassals (future use)
	SSHUser       string            `yaml:"ssh_user,omitempty"` // SSH user (future use)

	// Claude-specific (only used when Type == "claude")
	Model string `yaml:"model,omitempty"` // e.g. "claude-opus-4-6", "claude-haiku-4-5-20251001"

	// Serial-specific (only used when Type == "serial")
	SerialPort     string `yaml:"serial_port,omitempty"`
	BaudRate       int    `yaml:"baud_rate,omitempty"`
	SerialProtocol string `yaml:"serial_protocol,omitempty"` // "esp32" | "nmea" | "at" | "" (auto)

	// Guards — runtime health checks for this vassal
	Guards []GuardConfig `yaml:"guards,omitempty"`
}

// GuardConfig defines a single runtime health check for a vassal.
type GuardConfig struct {
	Type      string `yaml:"type"`                // "port_check" | "log_watch" | "data_rate" | "health_check"
	Interval  int    `yaml:"interval,omitempty"`  // seconds between checks; default 10
	Threshold int    `yaml:"threshold,omitempty"` // circuit breaker opens after N consecutive failures; default 3

	// port_check fields
	Port   int    `yaml:"port,omitempty"`   // TCP port to check (1–65535)
	Expect string `yaml:"expect,omitempty"` // "open" | "closed"; default "open"

	// log_watch fields
	FailOn []string `yaml:"fail_on,omitempty"` // regex patterns that trigger failure

	// data_rate fields
	Min string `yaml:"min,omitempty"` // minimum throughput, e.g. "100bps", "1.5kbps", "1mbps"

	// health_check fields
	Exec    string `yaml:"exec,omitempty"`    // path to script (relative to kingdom root)
	Timeout int    `yaml:"timeout,omitempty"` // script timeout in seconds; default 10

	// Compiled at validation time (not serialized)
	CompiledPatterns []*regexp.Regexp `yaml:"-"`
	MinBytesPerSec   float64          `yaml:"-"`
}

// AutostartOrDefault returns the autostart value, defaulting to true if not set.
func (v VassalConfig) AutostartOrDefault() bool {
	if v.Autostart == nil {
		return true
	}
	return *v.Autostart
}

// TypeOrDefault returns "shell" if Type is empty.
func (v VassalConfig) TypeOrDefault() string {
	if v.Type == "" {
		return "shell"
	}
	return v.Type
}

// BaudRateOrDefault returns the configured BaudRate or 115200 if not set.
func (v VassalConfig) BaudRateOrDefault() int {
	if v.BaudRate == 0 {
		return 115200
	}
	return v.BaudRate
}

// PatternConfig represents an event detection pattern
type PatternConfig struct {
	Name            string `yaml:"name"`
	Regex           string `yaml:"regex"`
	Severity        string `yaml:"severity"`
	Source          string `yaml:"source,omitempty"`
	SummaryTemplate string `yaml:"summary_template,omitempty"`
}

// Settings represents kingdom-level settings
type Settings struct {
	LogRetentionDays     int    `yaml:"log_retention_days,omitempty"`
	MaxOutputBuffer      string `yaml:"max_output_buffer,omitempty"`
	EventCooldownSeconds int    `yaml:"event_cooldown_seconds,omitempty"`

	// Audit settings
	AuditIngestion              bool `yaml:"audit_ingestion,omitempty"`
	AuditRetentionDays          int  `yaml:"audit_retention_days,omitempty"`
	AuditIngestionRetentionDays int  `yaml:"audit_ingestion_retention_days,omitempty"`
	SovereignApproval           bool `yaml:"sovereign_approval,omitempty"`
	SovereignApprovalTimeout    int  `yaml:"sovereign_approval_timeout,omitempty"`
	ScanExecOutput              bool `yaml:"scan_exec_output,omitempty"`
	AuditMaxTraceOutput         int  `yaml:"audit_max_trace_output,omitempty"`

	DefaultModel        string   `yaml:"default_model,omitempty"` // default model for all claude vassals
	SecurityScanner     string   `yaml:"security_scanner,omitempty"`
	SecurityScannerArgs []string `yaml:"security_scanner_args,omitempty"`
}

// DefaultSettings returns settings with sensible defaults.
func DefaultSettings() Settings {
	return Settings{
		LogRetentionDays:     7,
		MaxOutputBuffer:      "10MB",
		EventCooldownSeconds: 30,

		AuditIngestion:              false,
		AuditRetentionDays:          7,
		AuditIngestionRetentionDays: 1,
		SovereignApproval:           false,
		SovereignApprovalTimeout:    300,
		ScanExecOutput:              true,
		AuditMaxTraceOutput:         10000,
	}
}
