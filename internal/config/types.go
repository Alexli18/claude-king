package config

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
	AuditMaxTraceOutput         int  `yaml:"audit_max_trace_output,omitempty"`
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
		AuditMaxTraceOutput:         10000,
	}
}
