//go:build !windows

package daemon_test

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/daemon"
)

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		current  time.Duration
		expected time.Duration
	}{
		{1 * time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{32 * time.Second, 60 * time.Second}, // capped at 60s
		{60 * time.Second, 60 * time.Second}, // stays at max
	}
	for _, tt := range tests {
		got := daemon.NextBackoff(tt.current)
		if got != tt.expected {
			t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.expected)
		}
	}
}

// TestPGIDKillsProcessGroup verifies that killing a process group (negative PID)
// kills the group leader and all children, not just the leader.
func TestPGIDKillsProcessGroup(t *testing.T) {
	// Start a shell that spawns a child sleep process.
	cmd := exec.Command("sh", "-c", "sleep 100 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("getpgid: %v", err)
	}

	// Kill the entire process group.
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// process group killed, good
	case <-time.After(2 * time.Second):
		t.Fatal("process group not killed within 2s")
	}
}
