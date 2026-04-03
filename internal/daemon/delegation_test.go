package daemon

import (
	"testing"
	"time"
)

func TestDelegationHelpers(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
	}

	t.Run("not delegated initially", func(t *testing.T) {
		if d.isDelegated("ui") {
			t.Fatal("expected ui to not be delegated")
		}
	})

	t.Run("set and detect delegation", func(t *testing.T) {
		d.setDelegation("ui", 1234)
		if !d.isDelegated("ui") {
			t.Fatal("expected ui to be delegated after setDelegation")
		}
	})

	t.Run("heartbeat ack for correct pid", func(t *testing.T) {
		d.setDelegation("api", 5678)
		if !d.updateHeartbeat("api", 5678) {
			t.Fatal("expected heartbeat to be acknowledged")
		}
	})

	t.Run("heartbeat nack for wrong pid", func(t *testing.T) {
		d.setDelegation("db", 1111)
		if d.updateHeartbeat("db", 9999) {
			t.Fatal("expected heartbeat to be rejected for wrong pid")
		}
	})

	t.Run("heartbeat nack for unknown vassal", func(t *testing.T) {
		if d.updateHeartbeat("unknown", 1234) {
			t.Fatal("expected heartbeat to be rejected for unknown vassal")
		}
	})

	t.Run("release delegation", func(t *testing.T) {
		d.setDelegation("worker", 2222)
		d.releaseDelegation("worker")
		if d.isDelegated("worker") {
			t.Fatal("expected worker to not be delegated after release")
		}
	})

	t.Run("heartbeat updates timestamp", func(t *testing.T) {
		d.setDelegation("cache", 3333)
		before := d.delegatedVassals["cache"].LastHeartbeat
		time.Sleep(2 * time.Millisecond)
		d.updateHeartbeat("cache", 3333)
		after := d.delegatedVassals["cache"].LastHeartbeat
		if !after.After(before) {
			t.Fatal("expected LastHeartbeat to be updated")
		}
	})
}
