package daemon

import (
	"encoding/json"
	"time"
)

// guardStatusEntry is the JSON-serializable view of a GuardState.
type guardStatusEntry struct {
	VassalName       string    `json:"vassal_name"`
	GuardIndex       int       `json:"guard_index"`
	GuardType        string    `json:"guard_type"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	CircuitOpen      bool      `json:"circuit_open"`
	LastCheckTime    time.Time `json:"last_check_time"`
	LastResult       struct {
		OK        bool   `json:"ok"`
		Message   string `json:"message"`
		CheckedAt time.Time `json:"checked_at"`
	} `json:"last_result"`
}

// registerGuardHandlers adds the guard_status daemon RPC handler.
// Called from registerRealHandlers().
func registerGuardHandlers(d *Daemon) {
	d.handlers["guard_status"] = func(params json.RawMessage) (interface{}, error) {
		var req struct {
			Vassal string `json:"vassal"`
		}
		// Optional vassal filter; ignore unmarshal errors (params may be null).
		_ = json.Unmarshal(params, &req)

		d.guardStatesMu.RLock()
		defer d.guardStatesMu.RUnlock()

		entries := make([]guardStatusEntry, 0, len(d.guardStates))
		for _, gs := range d.guardStates {
			if req.Vassal != "" && gs.VassalName != req.Vassal {
				continue
			}
			entry := guardStatusEntry{
				VassalName:       gs.VassalName,
				GuardIndex:       gs.GuardIndex,
				GuardType:        gs.GuardType,
				ConsecutiveFails: gs.ConsecutiveFails,
				CircuitOpen:      gs.CircuitOpen,
				LastCheckTime:    gs.LastCheckTime,
			}
			entry.LastResult.OK = gs.LastResult.OK
			entry.LastResult.Message = gs.LastResult.Message
			entry.LastResult.CheckedAt = gs.LastResult.CheckedAt
			entries = append(entries, entry)
		}

		return map[string]interface{}{"guards": entries}, nil
	}
}
