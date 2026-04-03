package daemon

import (
	"encoding/json"
	"fmt"
)

// anyCircuitOpen returns true and a descriptive error message if any guard for
// the given vassal has an open circuit breaker. Reads guardStates under RLock.
func (d *Daemon) anyCircuitOpen(vassal string) (bool, string) {
	d.guardStatesMu.RLock()
	defer d.guardStatesMu.RUnlock()
	for _, gs := range d.guardStates {
		if gs.VassalName == vassal && gs.CircuitOpen {
			return true, fmt.Sprintf(
				"Guard '%s' (index %d) circuit open for vassal '%s'. Consecutive failures: %d. AI modifications blocked.",
				gs.GuardType, gs.GuardIndex, vassal, gs.ConsecutiveFails,
			)
		}
	}
	return false, ""
}

// registerDelegationHandlers adds delegate_control, delegate_heartbeat,
// and delegate_release to d.handlers. Called from registerRealHandlers().
func registerDelegationHandlers(d *Daemon) {
	d.handlers["delegate_control"] = func(params json.RawMessage) (interface{}, error) {
		var req struct {
			Vassal     string `json:"vassal"`
			SessionPID int    `json:"session_pid"`
			Force      bool   `json:"force"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Vassal == "" {
			return nil, fmt.Errorf("vassal is required")
		}

		d.delegationMu.Lock()
		defer d.delegationMu.Unlock()

		// Block delegation if a guard circuit breaker is open for this vassal.
		if open, reason := d.anyCircuitOpen(req.Vassal); open {
			return nil, fmt.Errorf("%s", reason)
		}

		if existing, ok := d.delegatedVassals[req.Vassal]; ok && !req.Force {
			return nil, fmt.Errorf("vassal %q is already delegated to PID %d; use force=true to override",
				req.Vassal, existing.SessionPID)
		}

		d.delegatedVassals[req.Vassal] = DelegationInfo{
			SessionPID:    req.SessionPID,
			LastHeartbeat: timeNow(),
		}
		d.logger.Info("DELEGATION_GRANTED", "vassal", req.Vassal, "session_pid", req.SessionPID)

		return map[string]interface{}{"ok": true, "vassal": req.Vassal}, nil
	}

	d.handlers["delegate_heartbeat"] = func(params json.RawMessage) (interface{}, error) {
		var req struct {
			Vassal     string `json:"vassal"`
			SessionPID int    `json:"session_pid"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}

		ack := d.updateHeartbeat(req.Vassal, req.SessionPID)
		status := "delegated"
		if !ack {
			status = "unknown"
		}
		return map[string]interface{}{
			"acknowledged": ack,
			"status":       status,
		}, nil
	}

	d.handlers["delegate_release"] = func(params json.RawMessage) (interface{}, error) {
		var req struct {
			Vassal string `json:"vassal"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		d.releaseDelegation(req.Vassal)
		d.logger.Info("DELEGATION_RELEASED", "vassal", req.Vassal)
		return map[string]interface{}{"ok": true}, nil
	}
}
