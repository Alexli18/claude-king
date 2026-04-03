# MCP Tool Contracts: King v2.1 Guards

These are the MCP tools that Claude Code (vassal) sessions can call via the King daemon.

## Existing Tools (Unchanged)

### `delegate_control`
Take exclusive control of a vassal.

**Input**:
```json
{
  "vassal": "string (required) — vassal name",
  "force": "boolean (optional, default false) — override existing delegation"
}
```

**Output (success)**:
```json
{ "ok": true, "vassal": "api" }
```

**Output (blocked — circuit breaker)**:
```json
{ "error": "Guard 'data_rate' circuit open for vassal 'esp32-collector'. AI modifications blocked. Check guard_status for details." }
```

**Output (blocked — already delegated)**:
```json
{ "error": "vassal \"api\" is already delegated to PID 4455; use force=true to override" }
```

---

### `delegate_release`
Return control of a vassal to the daemon.

**Input**:
```json
{ "vassal": "string (required)" }
```

**Output**:
```json
{ "ok": true }
```

---

### `delegate_status`
Show delegation status for this MCP session.

**Input**: none

**Output**:
```json
{
  "observer_mode": true,
  "parent_kingdom_socket": "/path/to/king-abc123.sock",
  "session_pid": 4455
}
```

---

## New Tools (v2.1)

### `guard_status`
Query the current health of all guards for a vassal (or all vassals).

**Input**:
```json
{
  "vassal": "string (optional) — filter by vassal name; omit for all vassals"
}
```

**Output**:
```json
{
  "guards": [
    {
      "vassal": "esp32-collector",
      "guard_index": 0,
      "type": "data_rate",
      "circuit_open": false,
      "consecutive_fails": 0,
      "last_check": "2026-04-03T23:50:00Z",
      "last_result": {
        "ok": true,
        "message": "data rate 1.2kbps ≥ 100bps threshold"
      }
    },
    {
      "vassal": "esp32-collector",
      "guard_index": 1,
      "type": "log_watch",
      "circuit_open": true,
      "consecutive_fails": 3,
      "last_check": "2026-04-03T23:49:55Z",
      "last_result": {
        "ok": false,
        "message": "pattern 'SerialException' matched in output"
      }
    }
  ]
}
```

**Error** (vassal not found):
```json
{ "error": "vassal 'unknown' not found in kingdom" }
```

---

## Daemon RPC Contracts (Unix Socket JSON-RPC)

These are internal RPC methods called by the MCP layer. Not directly exposed to users.

### `delegate_control` (daemon-side)

**Params**:
```json
{ "vassal": "string", "session_pid": 4455, "force": false }
```

**Guard check added in v2.1**: Before granting delegation, daemon checks if any guard for this vassal has `circuit_open == true`. If so, returns error instead of granting.

**Result (blocked by guard)**:
```json
{
  "error": "Guard 'log_watch' (index 1) circuit open for vassal 'esp32-collector'. Consecutive failures: 3. AI modifications blocked."
}
```

### `delegate_heartbeat` (daemon-side, unchanged)

**Params**: `{ "vassal": "string", "session_pid": 4455 }`

**Result**: `{ "acknowledged": true, "status": "delegated" }`

### `delegate_release` (daemon-side, unchanged)

**Params**: `{ "vassal": "string" }`

**Result**: `{ "ok": true }`

### `guard_status` (daemon-side, new)

**Params**: `{ "vassal": "string (optional)" }`

**Result**: Same as MCP `guard_status` output above.
