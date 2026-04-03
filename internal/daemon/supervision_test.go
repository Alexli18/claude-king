//go:build !windows

package daemon_test

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

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
