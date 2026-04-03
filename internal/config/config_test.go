package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

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
