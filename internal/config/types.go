package config

// KingdomConfig represents the top-level kingdom configuration from .king/kingdom.yml
type KingdomConfig struct {
	Name         string          `yaml:"name"`
	Vassals      []VassalConfig  `yaml:"vassals"`
	Patterns     []PatternConfig `yaml:"patterns"`
	ArtifactsDir string          `yaml:"artifacts_dir,omitempty"`
}

// VassalConfig represents a vassal definition in the kingdom configuration
type VassalConfig struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Cwd           string            `yaml:"cwd,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	Autostart     bool              `yaml:"autostart,omitempty"`
	RestartPolicy string            `yaml:"restart_policy,omitempty"`
}

// PatternConfig represents an event detection pattern
type PatternConfig struct {
	Name     string `yaml:"name"`
	Regex    string `yaml:"regex"`
	Severity string `yaml:"severity"`
}
