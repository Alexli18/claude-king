// internal/daemon/auto_integrity_test.go
package daemon_test

import (
	"testing"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/daemon"
)

func TestMergeAutoContracts_NoOverwrite(t *testing.T) {
	existing := []config.PatternConfig{
		{Name: "my-pattern", Regex: `error`, Severity: "error"},
	}
	auto := []config.PatternConfig{
		{Name: "go-vet-error", Regex: `\.go:\d+`, Severity: "error", Source: "auto"},
		{Name: "my-pattern", Regex: `duplicate`, Severity: "warning", Source: "auto"}, // duplicate
	}

	merged := daemon.MergeAutoContracts(existing, auto)

	if len(merged) != 2 {
		t.Errorf("expected 2 patterns, got %d: %+v", len(merged), merged)
	}
	// Existing "my-pattern" must not be overwritten.
	for _, p := range merged {
		if p.Name == "my-pattern" && p.Regex == "duplicate" {
			t.Error("existing pattern was overwritten by auto contract")
		}
	}
}

func TestMergeAutoContracts_AddsNew(t *testing.T) {
	existing := []config.PatternConfig{}
	auto := []config.PatternConfig{
		{Name: "go-vet-error", Regex: `\.go:\d+`, Severity: "error", Source: "auto"},
	}

	merged := daemon.MergeAutoContracts(existing, auto)
	if len(merged) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(merged))
	}
}
