// internal/daemon/auto_integrity.go
package daemon

import (
	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/fingerprint"
)

// MergeAutoContracts merges auto-generated contracts into existing patterns.
// Existing patterns are never overwritten (deduplication by name).
// Exported for testing.
func MergeAutoContracts(existing, auto []config.PatternConfig) []config.PatternConfig {
	names := make(map[string]bool, len(existing))
	for _, p := range existing {
		names[p.Name] = true
	}
	result := make([]config.PatternConfig, len(existing))
	copy(result, existing)
	for _, p := range auto {
		if !names[p.Name] {
			result = append(result, p)
			names[p.Name] = true
		}
	}
	return result
}

// injectAutoContracts fingerprints repoPath and merges integrity contracts
// into d.config.Patterns. Safe to call multiple times (idempotent via dedup).
func (d *Daemon) injectAutoContracts(repoPath string) {
	pt := fingerprint.Fingerprint(repoPath)
	contracts := fingerprint.DefaultContracts(pt, repoPath)
	if len(contracts) == 0 {
		return
	}
	d.config.Patterns = MergeAutoContracts(d.config.Patterns, contracts)
	d.logger.Info("auto-integrity contracts injected",
		"repo", repoPath,
		"project_type", string(pt),
		"contracts", len(contracts),
	)
}
