package daemon

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// checkPortOpen
// ---------------------------------------------------------------------------

func TestCheckPortOpen_Open(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	result := checkPortOpen(port, "open")
	if !result.OK {
		t.Fatalf("expected OK=true for open port, got: %s", result.Message)
	}
}

func TestCheckPortOpen_Closed(t *testing.T) {
	// Bind to a port then immediately close — port should be closed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	result := checkPortOpen(port, "open")
	if result.OK {
		t.Fatalf("expected OK=false for closed port, got: %s", result.Message)
	}
}

func TestCheckPortOpen_ExpectClosed(t *testing.T) {
	// No listener on a free port → OK because we expect closed.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	result := checkPortOpen(port, "closed")
	if !result.OK {
		t.Fatalf("expected OK=true when port is closed and we expect closed, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// checkHealthScript
// ---------------------------------------------------------------------------

func TestCheckHealthScript_Pass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "pass.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	result := checkHealthScript(script, 5*time.Second, dir)
	if !result.OK {
		t.Fatalf("expected OK=true for exit 0 script, got: %s", result.Message)
	}
}

func TestCheckHealthScript_Fail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	result := checkHealthScript(script, 5*time.Second, dir)
	if result.OK {
		t.Fatalf("expected OK=false for exit 1 script, got: %s", result.Message)
	}
}

func TestCheckHealthScript_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 10\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	result := checkHealthScript(script, 100*time.Millisecond, dir)
	if result.OK {
		t.Fatalf("expected OK=false for timed-out script")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty timeout message")
	}
}

// ---------------------------------------------------------------------------
// checkLogWatch (via Daemon method, using a stub PTY manager getter)
// ---------------------------------------------------------------------------

// stubLines is a simple helper daemon that returns predefined lines via
// ptyMgr.GetSessionRecentLines. We test checkLogWatch indirectly by invoking
// it directly with a test daemon whose ptyMgr is nil (which returns nil lines).
// A nil/empty line set should always return OK.
func TestCheckLogWatch_NoLines(t *testing.T) {
	d := newTestDaemon(t)
	// ptyMgr is nil → GetSessionRecentLines returns nil → no patterns to match.
	pat := regexp.MustCompile("ERROR")
	result := d.checkLogWatch("api", []*regexp.Regexp{pat}, 10*time.Second)
	if !result.OK {
		t.Fatalf("expected OK=true when no output lines exist, got: %s", result.Message)
	}
}

func TestCheckLogWatch_PatternMatch(t *testing.T) {
	// We test the core logic of checkLogWatch by calling it with a daemon that
	// has a real PTY manager and a pre-seeded session's recentLines.
	// Since ptyMgr.GetSessionRecentLines is nil-safe, we cannot inject lines
	// without a running session. Instead we verify the ANSI-stripping logic and
	// the pattern match path via a standalone helper.
	lines := []string{
		"normal output",
		"\x1b[31mERROR\x1b[0m: something broke",
	}
	pat := regexp.MustCompile("ERROR")
	for _, line := range lines {
		clean := ansiStrip.ReplaceAllString(line, "")
		if pat.MatchString(clean) {
			// Found a match — this is the expected path for the ANSI-escaped ERROR line.
			return
		}
	}
	t.Fatal("expected ansiStrip to expose ERROR in ANSI-escaped line")
}

// ---------------------------------------------------------------------------
// guard_status handler
// ---------------------------------------------------------------------------

func TestGuardStatusHandler(t *testing.T) {
	d := newTestDaemon(t)
	d.handlers = make(map[string]rpcHandler)
	registerGuardHandlers(d)

	d.guardStatesMu.Lock()
	d.guardStates["api:0"] = &GuardState{
		VassalName:       "api",
		GuardIndex:       0,
		GuardType:        "port_check",
		ConsecutiveFails: 2,
		CircuitOpen:      true,
		LastCheckTime:    time.Now(),
		LastResult:       GuardResult{OK: false, Message: "port closed", CheckedAt: time.Now()},
	}
	d.guardStatesMu.Unlock()

	result, err := d.handlers["guard_status"](nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]interface{})
	guards, ok := m["guards"]
	if !ok {
		t.Fatal("expected 'guards' key in result")
	}
	list := guards.([]guardStatusEntry)
	if len(list) != 1 {
		t.Fatalf("expected 1 guard entry, got %d", len(list))
	}
	if !list[0].CircuitOpen {
		t.Fatal("expected CircuitOpen=true")
	}
	if list[0].VassalName != "api" {
		t.Fatalf("expected vassal_name=api, got %q", list[0].VassalName)
	}
}

// ---------------------------------------------------------------------------
// checkDataRate
// ---------------------------------------------------------------------------

func TestCheckDataRate_AboveMin(t *testing.T) {
	result := checkDataRate(1100, 100, 100.0, 10) // (1100-100)/10 = 100 B/s == min
	if !result.OK {
		t.Fatalf("expected OK=true at exactly min rate, got: %s", result.Message)
	}
}

func TestCheckDataRate_BelowMin(t *testing.T) {
	result := checkDataRate(200, 100, 200.0, 10) // (200-100)/10 = 10 B/s < 200 min
	if result.OK {
		t.Fatalf("expected OK=false below min rate, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// Circuit breaker state machine
// ---------------------------------------------------------------------------

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	return &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
}

func TestCircuitBreaker_Opens(t *testing.T) {
	d := newTestDaemon(t)
	key := "api:0"
	d.guardStates[key] = &GuardState{VassalName: "api", GuardIndex: 0, GuardType: "port_check"}
	threshold := 3

	for i := 0; i < threshold-1; i++ {
		d.updateGuardState(key, GuardResult{OK: false, Message: "fail", CheckedAt: time.Now()}, threshold)
		d.guardStatesMu.RLock()
		open := d.guardStates[key].CircuitOpen
		d.guardStatesMu.RUnlock()
		if open {
			t.Fatalf("circuit should not be open after only %d failures (threshold %d)", i+1, threshold)
		}
	}
	// threshold-th failure should open circuit
	d.updateGuardState(key, GuardResult{OK: false, Message: "fail", CheckedAt: time.Now()}, threshold)
	d.guardStatesMu.RLock()
	gs := d.guardStates[key]
	circuitOpen := gs.CircuitOpen
	consecFails := gs.ConsecutiveFails
	d.guardStatesMu.RUnlock()
	if !circuitOpen {
		t.Fatalf("expected circuit open after %d consecutive failures", threshold)
	}
	if consecFails != threshold {
		t.Fatalf("expected ConsecutiveFails=%d, got %d", threshold, consecFails)
	}
}

func TestCircuitBreaker_Recovers(t *testing.T) {
	d := newTestDaemon(t)
	key := "api:0"
	d.guardStates[key] = &GuardState{
		VassalName:       "api",
		GuardIndex:       0,
		GuardType:        "port_check",
		ConsecutiveFails: 3,
		CircuitOpen:      true,
	}

	d.updateGuardState(key, GuardResult{OK: true, Message: "pass", CheckedAt: time.Now()}, 3)
	d.guardStatesMu.RLock()
	gs := d.guardStates[key]
	circuitOpen := gs.CircuitOpen
	consecFails := gs.ConsecutiveFails
	d.guardStatesMu.RUnlock()
	if circuitOpen {
		t.Fatal("expected circuit closed after a passing check")
	}
	if consecFails != 0 {
		t.Fatalf("expected ConsecutiveFails=0 after recovery, got %d", consecFails)
	}
}
