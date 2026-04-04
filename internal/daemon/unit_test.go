package daemon

import (
	"os"
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

// ---------------------------------------------------------------------------
// mergePatterns (unexported)
// ---------------------------------------------------------------------------

func TestMergePatterns_NoDuplicates(t *testing.T) {
	existing := []config.PatternConfig{
		{Name: "error", Regex: "error"},
	}
	newPats := []config.PatternConfig{
		{Name: "warning", Regex: "warn"},
	}

	result := mergePatterns(existing, newPats)
	if len(result) != 2 {
		t.Errorf("expected 2 patterns, got %d", len(result))
	}
}

func TestMergePatterns_SkipsDuplicates(t *testing.T) {
	existing := []config.PatternConfig{
		{Name: "error", Regex: "error"},
		{Name: "warning", Regex: "warn"},
	}
	newPats := []config.PatternConfig{
		{Name: "error", Regex: "ERR"}, // same name — should be skipped
		{Name: "panic", Regex: "panic"},
	}

	result := mergePatterns(existing, newPats)
	if len(result) != 3 {
		t.Errorf("expected 3 patterns (deduplicated), got %d", len(result))
	}
	// Verify the existing "error" regex wasn't overwritten
	for _, p := range result {
		if p.Name == "error" && p.Regex != "error" {
			t.Errorf("expected original error regex to be kept, got %q", p.Regex)
		}
	}
}

func TestMergePatterns_EmptyExisting(t *testing.T) {
	newPats := []config.PatternConfig{
		{Name: "error", Regex: "error"},
	}
	result := mergePatterns(nil, newPats)
	if len(result) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(result))
	}
}

func TestMergePatterns_EmptyNew(t *testing.T) {
	existing := []config.PatternConfig{
		{Name: "error", Regex: "error"},
	}
	result := mergePatterns(existing, nil)
	if len(result) != 1 {
		t.Errorf("expected 1 pattern unchanged, got %d", len(result))
	}
}

func TestMergePatterns_BothEmpty(t *testing.T) {
	result := mergePatterns(nil, nil)
	if len(result) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// processAlive (unexported)
// ---------------------------------------------------------------------------

func TestProcessAlive_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	if !processAlive(pid) {
		t.Errorf("expected current process (pid=%d) to be alive", pid)
	}
}

func TestProcessAlive_InvalidPID(t *testing.T) {
	// PID 999999999 should not exist
	if processAlive(999999999) {
		t.Error("expected non-existent PID to be reported as not alive")
	}
}

func TestProcessAlive_ZeroPID(t *testing.T) {
	// PID 0 is not a valid user process
	// On macOS/Linux, kill(0, 0) sends to the whole process group, which
	// succeeds; so we just test it doesn't panic.
	_ = processAlive(0)
}

// ---------------------------------------------------------------------------
// VassalClientPool (exported — accessible from internal test)
// ---------------------------------------------------------------------------

func TestVassalClientPool_EmptyNames(t *testing.T) {
	pool := NewVassalClientPool()
	names := pool.Names()
	if len(names) != 0 {
		t.Errorf("expected empty names, got %v", names)
	}
}

func TestVassalClientPool_GetMissing(t *testing.T) {
	pool := NewVassalClientPool()
	vc, ok := pool.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for missing vassal")
	}
	if vc != nil {
		t.Error("expected nil client for missing vassal")
	}
}

func TestVassalClientPool_DisconnectAllEmpty(t *testing.T) {
	pool := NewVassalClientPool()
	pool.DisconnectAll() // must not panic on empty pool
}

func TestVassalClientPool_Disconnect_Missing(t *testing.T) {
	pool := NewVassalClientPool()
	err := pool.Disconnect("nonexistent")
	if err == nil {
		t.Fatal("expected error when disconnecting non-existent vassal, got nil")
	}
}
