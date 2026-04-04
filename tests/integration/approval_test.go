//go:build integration

package integration_test

import (
	"encoding/json"
	"testing"
	"time"
)

func TestApproval_Approved(t *testing.T) {
	td := startDaemon(t, approvalKingdom())
	time.Sleep(300 * time.Millisecond)

	// exec_in will block waiting for approval — run it in a goroutine.
	// Client.Call holds a mutex for the full request lifecycle, so we need
	// a separate client for polling and responding while exec_in blocks.
	pollClient := newPollClient(t, td.sockPath)

	type execResult struct {
		raw json.RawMessage
		err error
	}
	resultCh := make(chan execResult, 1)

	go func() {
		raw, err := td.client.Call("exec_in", map[string]interface{}{
			"target":          "shell",
			"command":         "echo APPROVAL_TEST_OUTPUT",
			"timeout_seconds": 8,
		})
		resultCh <- execResult{raw, err}
	}()

	// Poll for the pending approval request using the separate client.
	var approvalID string
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		raw, err := pollClient.Call("list_pending_approvals", nil)
		if err != nil {
			continue
		}
		var resp struct {
			Approvals []struct {
				ID      string `json:"id"`
				Command string `json:"command"`
			} `json:"approvals"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			continue
		}
		for _, a := range resp.Approvals {
			if a.Command == "echo APPROVAL_TEST_OUTPUT" {
				approvalID = a.ID
				break
			}
		}
		if approvalID != "" {
			break
		}
	}

	if approvalID == "" {
		t.Fatal("timed out waiting for approval request to appear")
	}
	t.Logf("found approval request: %s", approvalID)

	// Approve it via the poll client (main client is still blocked in exec_in).
	raw2, err := pollClient.Call("respond_approval", map[string]interface{}{
		"request_id": approvalID,
		"approved":   true,
	})
	if err != nil {
		t.Fatalf("respond_approval failed: %v", err)
	}
	_ = raw2

	// Wait for exec_in to complete
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("exec_in failed after approval: %v", result.err)
		}
		var resp struct {
			Output   string `json:"output"`
			ExitCode int    `json:"exit_code"`
		}
		mustUnmarshal(t, result.raw, &resp)
		if resp.ExitCode != 0 {
			t.Errorf("expected exit_code=0, got %d", resp.ExitCode)
		}
		t.Logf("exec output: %q", resp.Output)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for exec_in to complete after approval")
	}
}

func TestApproval_Denied(t *testing.T) {
	td := startDaemon(t, approvalKingdom())
	time.Sleep(300 * time.Millisecond)

	// Separate client for polling/responding while exec_in blocks the main client.
	pollClient := newPollClient(t, td.sockPath)

	type execResult struct {
		raw json.RawMessage
		err error
	}
	resultCh := make(chan execResult, 1)

	go func() {
		raw, err := td.client.Call("exec_in", map[string]interface{}{
			"target":          "shell",
			"command":         "echo SHOULD_NOT_RUN",
			"timeout_seconds": 8,
		})
		resultCh <- execResult{raw, err}
	}()

	// Poll for pending approval using the separate client.
	var approvalID string
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		raw, err := pollClient.Call("list_pending_approvals", nil)
		if err != nil {
			continue
		}
		var resp struct {
			Approvals []struct {
				ID      string `json:"id"`
				Command string `json:"command"`
			} `json:"approvals"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			continue
		}
		for _, a := range resp.Approvals {
			if a.Command == "echo SHOULD_NOT_RUN" {
				approvalID = a.ID
				break
			}
		}
		if approvalID != "" {
			break
		}
	}

	if approvalID == "" {
		t.Fatal("timed out waiting for approval request")
	}

	// Deny it via the poll client.
	if _, err := pollClient.Call("respond_approval", map[string]interface{}{
		"request_id": approvalID,
		"approved":   false,
	}); err != nil {
		t.Fatalf("respond_approval failed: %v", err)
	}

	// exec_in should return an error
	select {
	case result := <-resultCh:
		if result.err == nil {
			t.Error("expected exec_in to fail when approval denied, got nil error")
		} else {
			t.Logf("correctly rejected: %v", result.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for exec_in to complete after denial")
	}
}
