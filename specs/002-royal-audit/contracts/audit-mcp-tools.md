# MCP Tools Contract: Royal Audit

**Feature**: 002-royal-audit
**Date**: 2026-04-02

## Tool: get_audit_log

Retrieves audit entries with optional filters.

### Input Schema

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| layer | string | no | (all) | Filter by layer: "ingestion", "sieve", "action" |
| vassal | string | no | (all) | Filter by vassal name |
| since | string | no | (none) | Start time (RFC3339 or relative: "5m", "1h", "1d") |
| until | string | no | (none) | End time (RFC3339 or relative) |
| trace_id | string | no | (none) | Filter by specific Trace ID |
| limit | integer | no | 50 | Max entries to return (1-500) |

### Output Schema

```json
{
  "entries": [
    {
      "id": "uuid",
      "layer": "ingestion|sieve|action",
      "source": "vassal-name",
      "content": "line content or sieve decision or action description",
      "trace_id": "abc12345",
      "metadata": {},
      "sampled": false,
      "created_at": "2026-04-02 14:05:00"
    }
  ],
  "total": 150,
  "filtered": true,
  "kingdom_id": "uuid"
}
```

### Error Cases

| Condition | Error |
|-----------|-------|
| Invalid layer value | "invalid layer: must be ingestion, sieve, or action" |
| Invalid time format | "invalid time format for 'since': expected RFC3339 or relative (5m, 1h, 1d)" |
| Limit out of range | "limit must be between 1 and 500" |

---

## Tool: get_action_trace

Retrieves detailed Action Trace for a specific exec_in execution.

### Input Schema

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| trace_id | string | yes | - | Trace ID of the action |

### Output Schema

```json
{
  "trace_id": "abc12345",
  "vassal_name": "shell",
  "command": "npm test",
  "status": "completed|failed|timeout|running",
  "exit_code": 0,
  "output": "...",
  "duration_ms": 1234,
  "trigger_event": {
    "id": "event-uuid",
    "severity": "error",
    "pattern": "generic-error",
    "summary": "Error detected..."
  },
  "started_at": "2026-04-02 14:05:00",
  "completed_at": "2026-04-02 14:05:01",
  "approval": {
    "status": "approved",
    "responded_at": "2026-04-02 14:05:00"
  }
}
```

### Error Cases

| Condition | Error |
|-----------|-------|
| trace_id not provided | "trace_id is required" |
| Trace not found | "action trace not found: {trace_id}" |

---

## Tool: respond_approval (Sovereign Approval)

Responds to a pending Sovereign Approval request. Only available when sovereign_approval is enabled.

### Input Schema

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| request_id | string | yes | - | ID of the approval request |
| approved | boolean | yes | - | true to approve, false to reject |
| reason | string | no | "" | Optional reason for rejection |

### Output Schema

```json
{
  "request_id": "uuid",
  "trace_id": "abc12345",
  "status": "approved|rejected",
  "command": "npm test",
  "vassal_name": "shell"
}
```

### Error Cases

| Condition | Error |
|-----------|-------|
| request_id not provided | "request_id is required" |
| Request not found | "approval request not found: {request_id}" |
| Request not pending | "approval request is not pending (current: {status})" |
| Sovereign approval disabled | "sovereign_approval is not enabled" |

---

## RPC Methods (UDS JSON-RPC)

These methods are exposed via the daemon's Unix Domain Socket for `kingctl` CLI.

### get_audit_log

Same schema as MCP tool `get_audit_log`.

### get_action_trace

Same schema as MCP tool `get_action_trace`.

### respond_approval

Same schema as MCP tool `respond_approval`.

### list_pending_approvals

Returns all pending approval requests.

**Input**: `{}` (no parameters)

**Output**:
```json
{
  "requests": [
    {
      "id": "uuid",
      "trace_id": "abc12345",
      "command": "npm test",
      "vassal_name": "shell",
      "reason": "Error detected in logs",
      "created_at": "2026-04-02 14:05:00"
    }
  ]
}
```

---

## Config Contract (kingdom.yml)

```yaml
settings:
  # Existing settings...
  log_retention_days: 7
  max_output_buffer: "10MB"
  event_cooldown_seconds: 30

  # New audit settings
  audit_ingestion: false           # Enable raw output recording
  audit_retention_days: 7          # Sieve/Action retention (days)
  audit_ingestion_retention_days: 1 # Ingestion retention (days)
  sovereign_approval: false        # Require approval for exec_in
  sovereign_approval_timeout: 300  # Approval timeout (seconds)
  audit_max_trace_output: 10000   # Max chars in ActionTrace output
```
