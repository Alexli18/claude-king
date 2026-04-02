package config

// VassalManifest represents the VMP (Vassal Manifest Protocol) structure from vassal.json
type VassalManifest struct {
	Name         string                `json:"name"`
	Version      string                `json:"version"`
	Description  string                `json:"description,omitempty"`
	Skills       []Skill               `json:"skills,omitempty"`
	Artifacts    []ArtifactDecl        `json:"artifacts,omitempty"`
	Dependencies []string              `json:"dependencies,omitempty"`
	Config       VassalManifestConfig  `json:"config,omitempty"`
}

// Skill represents a declared capability of a vassal
type Skill struct {
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	Description string   `json:"description,omitempty"`
	Inputs      []string `json:"inputs,omitempty"`
	Outputs     []string `json:"outputs,omitempty"`
}

// ArtifactDecl represents a declared artifact output from a vassal
type ArtifactDecl struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// VassalManifestConfig represents configuration options in the vassal manifest
type VassalManifestConfig struct {
	Autostart     bool              `json:"autostart,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
}
