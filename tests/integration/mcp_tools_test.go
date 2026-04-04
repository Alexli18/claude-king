//go:build integration

package integration_test

import (
	"strings"
	"testing"
	"time"
)

func TestExecIn_SimpleCommand(t *testing.T) {
	td := startDaemon(t, minimalKingdom())

	// Give the PTY session a moment to start
	time.Sleep(200 * time.Millisecond)

	raw := td.call(t, "exec_in", map[string]interface{}{
		"target":          "shell",
		"command":         "echo INTEGRATION_HELLO",
		"timeout_seconds": 5,
	})

	var resp struct {
		Output   string `json:"output"`
		ExitCode int    `json:"exit_code"`
	}
	mustUnmarshal(t, raw, &resp)

	if resp.ExitCode != 0 {
		t.Errorf("expected exit_code=0, got %d (output: %q)", resp.ExitCode, resp.Output)
	}
	if !strings.Contains(resp.Output, "INTEGRATION_HELLO") {
		t.Errorf("expected output to contain INTEGRATION_HELLO, got: %q", resp.Output)
	}
	t.Logf("output: %q exit_code: %d", resp.Output, resp.ExitCode)
}

func TestExecIn_NonZeroExitCode(t *testing.T) {
	td := startDaemon(t, minimalKingdom())
	time.Sleep(200 * time.Millisecond)

	// Use a subshell so the parent PTY shell stays alive (exit 42 would kill it).
	raw := td.call(t, "exec_in", map[string]interface{}{
		"target":          "shell",
		"command":         "(exit 42)",
		"timeout_seconds": 5,
	})

	var resp struct {
		ExitCode int `json:"exit_code"`
	}
	mustUnmarshal(t, raw, &resp)

	if resp.ExitCode != 42 {
		t.Errorf("expected exit_code=42, got %d", resp.ExitCode)
	}
}

func TestExecIn_VassalNotFound(t *testing.T) {
	td := startDaemon(t, minimalKingdom())

	td.callExpectError(t, "exec_in", map[string]interface{}{
		"target":  "nonexistent-vassal",
		"command": "echo hi",
	}, "VASSAL_NOT_FOUND")
}

func TestExecIn_Timeout(t *testing.T) {
	td := startDaemon(t, minimalKingdom())
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	td.callExpectError(t, "exec_in", map[string]interface{}{
		"target":          "shell",
		"command":         "sleep 60",
		"timeout_seconds": 2,
	}, "")
	elapsed := time.Since(start)

	// Should have returned within ~3s (2s timeout + overhead)
	if elapsed > 5*time.Second {
		t.Errorf("exec_in timeout took too long: %v", elapsed)
	}
	t.Logf("timeout returned in %v", elapsed)
}

func TestGetEvents_AfterPatternMatch(t *testing.T) {
	td := startDaemon(t, eventKingdom())
	time.Sleep(300 * time.Millisecond)

	// Emit output that matches the configured pattern "KING_TEST_EVENT"
	td.call(t, "exec_in", map[string]interface{}{
		"target":          "shell",
		"command":         "echo KING_TEST_EVENT",
		"timeout_seconds": 5,
	})

	// Give the sieve a moment to process and persist the event
	time.Sleep(500 * time.Millisecond)

	raw := td.call(t, "get_events", map[string]interface{}{
		"severity": "error",
		"limit":    10,
	})

	var resp struct {
		Events []struct {
			Source   string `json:"source"`
			Severity string `json:"severity"`
			Summary  string `json:"summary"`
			Pattern  string `json:"pattern"`
		} `json:"events"`
		Count int `json:"count"`
	}
	mustUnmarshal(t, raw, &resp)

	if resp.Count == 0 {
		t.Fatal("expected at least one event after pattern match, got 0")
	}

	found := false
	for _, e := range resp.Events {
		if e.Pattern == "test-pattern" && e.Severity == "error" {
			found = true
			t.Logf("found event: source=%s severity=%s summary=%q", e.Source, e.Severity, e.Summary)
			break
		}
	}
	if !found {
		t.Errorf("expected event with pattern=test-pattern, got: %+v", resp.Events)
	}
}
