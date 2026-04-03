package daemon

import (
	"encoding/json"
	"testing"
)

func TestDelegateControlHandler(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		logger:           newTestLogger(t),
	}
	d.handlers = make(map[string]rpcHandler)
	registerDelegationHandlers(d)

	t.Run("delegate unknown vassal succeeds", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"vassal":      "ui",
			"session_pid": 1234,
			"force":       false,
		})
		result, err := d.handlers["delegate_control"](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := result.(map[string]interface{})
		if m["ok"] != true {
			t.Fatalf("expected ok=true, got %v", m)
		}
		if !d.isDelegated("ui") {
			t.Fatal("expected ui to be delegated")
		}
	})

	t.Run("delegate already-delegated vassal without force returns error", func(t *testing.T) {
		d.setDelegation("api", 9999)
		params, _ := json.Marshal(map[string]interface{}{
			"vassal":      "api",
			"session_pid": 1234,
			"force":       false,
		})
		_, err := d.handlers["delegate_control"](params)
		if err == nil {
			t.Fatal("expected error when delegating already-delegated vassal without force")
		}
	})

	t.Run("force-delegate takes over", func(t *testing.T) {
		d.setDelegation("worker", 9999)
		params, _ := json.Marshal(map[string]interface{}{
			"vassal":      "worker",
			"session_pid": 1234,
			"force":       true,
		})
		result, err := d.handlers["delegate_control"](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := result.(map[string]interface{})
		if m["ok"] != true {
			t.Fatalf("expected ok=true, got %v", m)
		}
	})
}

func TestDelegateHeartbeatHandler(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		logger:           newTestLogger(t),
	}
	d.handlers = make(map[string]rpcHandler)
	registerDelegationHandlers(d)

	d.setDelegation("ui", 1234)

	t.Run("known vassal returns acknowledged=true", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"vassal":      "ui",
			"session_pid": 1234,
		})
		result, err := d.handlers["delegate_heartbeat"](params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := result.(map[string]interface{})
		if m["acknowledged"] != true {
			t.Fatalf("expected acknowledged=true, got %v", m)
		}
	})

	t.Run("unknown vassal returns acknowledged=false", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"vassal":      "unknown",
			"session_pid": 9999,
		})
		result, _ := d.handlers["delegate_heartbeat"](params)
		m := result.(map[string]interface{})
		if m["acknowledged"] != false {
			t.Fatalf("expected acknowledged=false, got %v", m)
		}
	})
}

func TestDelegateReleaseHandler(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		logger:           newTestLogger(t),
	}
	d.handlers = make(map[string]rpcHandler)
	registerDelegationHandlers(d)

	d.setDelegation("ui", 1234)

	params, _ := json.Marshal(map[string]interface{}{"vassal": "ui"})
	result, err := d.handlers["delegate_release"](params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]interface{})
	if m["ok"] != true {
		t.Fatalf("expected ok=true")
	}
	if d.isDelegated("ui") {
		t.Fatal("expected ui to not be delegated after release")
	}
}
