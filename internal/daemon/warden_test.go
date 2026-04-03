package daemon

import (
	"testing"
	"time"
)

func TestWardenReleasesStaleVassal(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		logger:           newTestLogger(t),
	}

	// Delegate a vassal with a stale heartbeat
	d.delegatedVassals["ui"] = DelegationInfo{
		SessionPID:    9999,
		LastHeartbeat: time.Now().Add(-60 * time.Second), // stale
	}

	// Run a single warden tick
	d.wardenTick(30 * time.Second)

	if d.isDelegated("ui") {
		t.Fatal("expected warden to release stale delegation")
	}
}

func TestWardenKeepsFreshVassal(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		logger:           newTestLogger(t),
	}

	d.delegatedVassals["api"] = DelegationInfo{
		SessionPID:    1234,
		LastHeartbeat: time.Now(), // fresh
	}

	d.wardenTick(30 * time.Second)

	if !d.isDelegated("api") {
		t.Fatal("expected warden to keep fresh delegation")
	}
}
