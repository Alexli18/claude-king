//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/daemon"
)

// testDaemon holds a running daemon and its socket path for integration tests.
type testDaemon struct {
	sockPath string
	client   *daemon.Client
}

// startDaemon starts a real daemon with the given kingdom.yml content.
// Uses /tmp to avoid macOS 104-char Unix socket path limit.
func startDaemon(t *testing.T, kingdomYML string) *testDaemon {
	t.Helper()

	// Use /tmp explicitly to stay within macOS 104-char Unix socket path limit.
	rootDir, err := os.MkdirTemp("/tmp", "king-integration-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(rootDir) })

	// Write .king/kingdom.yml (LoadOrCreateConfig reads from .king/, not root).
	kingDir := filepath.Join(rootDir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(kingdomYML), 0644); err != nil {
		t.Fatalf("WriteFile kingdom.yml: %v", err)
	}

	d, err := daemon.NewDaemon(rootDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Start(ctx); err != nil {
		cancel()
		t.Fatalf("daemon.Start: %v", err)
	}

	sockPath := daemon.SocketPathForRoot(rootDir)
	client, err := daemon.NewClientFromSocket(sockPath)
	if err != nil {
		cancel()
		_ = d.Stop()
		t.Fatalf("NewClientFromSocket: %v", err)
	}

	t.Cleanup(func() {
		client.Close()
		cancel()
		_ = d.Stop()
	})

	return &testDaemon{sockPath: sockPath, client: client}
}

// call sends an RPC and returns the raw result. Fails the test on any error.
func (td *testDaemon) call(t *testing.T, method string, params interface{}) json.RawMessage {
	t.Helper()
	raw, err := td.client.Call(method, params)
	if err != nil {
		t.Fatalf("RPC %s failed: %v", method, err)
	}
	return raw
}

// callExpectError sends an RPC and asserts it returns an error containing substr.
func (td *testDaemon) callExpectError(t *testing.T, method string, params interface{}, substr string) {
	t.Helper()
	_, err := td.client.Call(method, params)
	if err == nil {
		t.Fatalf("RPC %s: expected error containing %q, got nil", method, substr)
	}
	if substr != "" && !contains(err.Error(), substr) {
		t.Fatalf("RPC %s error %q does not contain %q", method, err.Error(), substr)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

// mustUnmarshal decodes JSON into v or fails the test.
func mustUnmarshal(t *testing.T, raw json.RawMessage, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, raw)
	}
}

// minimalKingdom returns a kingdom.yml with a shell vassal and no approval.
func minimalKingdom() string {
	return fmt.Sprintf(`name: integration-test
vassals:
  - name: shell
    command: %s
    autostart: true
settings:
  sovereign_approval: false
`, shellBin())
}

// approvalKingdom returns a kingdom.yml with sovereign_approval enabled.
func approvalKingdom() string {
	return fmt.Sprintf(`name: integration-test-approval
vassals:
  - name: shell
    command: %s
    autostart: true
settings:
  sovereign_approval: true
  sovereign_approval_timeout: 10
`, shellBin())
}

// eventKingdom returns a kingdom.yml with a pattern that matches "KING_TEST_EVENT".
func eventKingdom() string {
	return fmt.Sprintf(`name: integration-test-events
vassals:
  - name: shell
    command: %s
    autostart: true
patterns:
  - name: test-pattern
    regex: 'KING_TEST_EVENT'
    severity: error
    summary_template: 'Test event detected in {vassal}'
settings:
  sovereign_approval: false
`, shellBin())
}

func shellBin() string {
	if _, err := os.Stat("/bin/sh"); err == nil {
		return "/bin/sh"
	}
	return "/bin/bash"
}
