//go:build integration

package integration_test

import (
	"encoding/json"
	"testing"
)

func TestDaemonLifecycle_KingdomStatus(t *testing.T) {
	td := startDaemon(t, minimalKingdom())

	raw := td.call(t, "kingdom.status", nil)

	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		PID    int    `json:"pid"`
	}
	mustUnmarshal(t, raw, &resp)

	if resp.ID == "" {
		t.Error("expected non-empty kingdom ID")
	}
	if resp.Name == "" {
		t.Error("expected non-empty kingdom name")
	}
	if resp.Status == "" {
		t.Error("expected non-empty status")
	}
	if resp.PID == 0 {
		t.Error("expected non-zero PID")
	}
	t.Logf("kingdom: id=%s name=%s status=%s pid=%d", resp.ID, resp.Name, resp.Status, resp.PID)
}

func TestDaemonLifecycle_UnknownMethod(t *testing.T) {
	td := startDaemon(t, minimalKingdom())
	td.callExpectError(t, "nonexistent.method", nil, "")
}

func TestDaemonLifecycle_ListVassals(t *testing.T) {
	td := startDaemon(t, minimalKingdom())

	raw := td.call(t, "list_vassals", nil)

	var resp struct {
		Vassals []json.RawMessage `json:"vassals"`
	}
	mustUnmarshal(t, raw, &resp)

	// minimalKingdom has autostart=true for "shell", so it should appear
	if len(resp.Vassals) == 0 {
		t.Error("expected at least one vassal (shell autostart=true)")
	}
	t.Logf("vassals count: %d", len(resp.Vassals))
}
