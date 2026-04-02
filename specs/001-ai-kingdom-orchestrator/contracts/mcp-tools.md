# MCP Tool Contracts: Scepter Tools

**Date**: 2026-04-02 | **Protocol**: MCP (Model Context Protocol)

These tools are exposed by the King daemon's MCP server and injected into Claude Code's tool context.

---

## list_vassals

List all vassal sessions in the current Kingdom with their status.

**Parameters**: None

**Response**:
```json
{
  "vassals": [
    {
      "name": "api-server",
      "status": "running",
      "command": "go run ./cmd/server",
      "pid": 12345,
      "last_activity": "2026-04-02T10:30:00Z"
    },
    {
      "name": "esp32-monitor",
      "status": "idle",
      "command": "minicom -D /dev/ttyUSB0",
      "pid": 12346,
      "last_activity": "2026-04-02T10:29:55Z"
    }
  ],
  "kingdom": {
    "name": "my-project",
    "status": "running",
    "uptime_seconds": 3600
  }
}
```

**Errors**:
- `KINGDOM_NOT_RUNNING`: No active Kingdom in current directory

---

## exec_in

Execute a command in a target vassal's PTY session.

**Parameters**:
```json
{
  "target": {
    "type": "string",
    "description": "Vassal name to execute in",
    "required": true
  },
  "command": {
    "type": "string",
    "description": "Shell command to execute",
    "required": true
  },
  "timeout_seconds": {
    "type": "integer",
    "description": "Max seconds to wait for output (0 = no timeout, default: 30)",
    "required": false
  },
  "stream": {
    "type": "boolean",
    "description": "If true, stream output incrementally (default: false)",
    "required": false
  }
}
```

**Response** (non-streaming):
```json
{
  "output": "Build successful\n2 packages compiled\n",
  "exit_code": 0,
  "duration_ms": 1523
}
```

**Response** (streaming): Incremental text chunks via MCP streaming.

**Errors**:
- `VASSAL_NOT_FOUND`: Target vassal does not exist
- `VASSAL_BUSY`: Target vassal is already executing a queued command (returns queue position)
- `TIMEOUT`: Command exceeded timeout_seconds
- `KINGDOM_NOT_RUNNING`: No active Kingdom

---

## read_neighbor

Read a file from a neighboring project directory (requires explicit permission).

**Parameters**:
```json
{
  "path": {
    "type": "string",
    "description": "Absolute or relative path to the file",
    "required": true
  },
  "max_lines": {
    "type": "integer",
    "description": "Maximum lines to return (default: 200)",
    "required": false
  }
}
```

**Response**:
```json
{
  "content": "file content here...",
  "path": "/absolute/resolved/path",
  "lines": 42,
  "truncated": false
}
```

**Errors**:
- `FILE_NOT_FOUND`: Path does not exist
- `PERMISSION_DENIED`: Path is outside allowed directories
- `KINGDOM_NOT_RUNNING`: No active Kingdom

---

## get_events

Retrieve recent events detected by the Semantic Sieve.

**Parameters**:
```json
{
  "severity": {
    "type": "string",
    "enum": ["info", "warning", "error", "critical"],
    "description": "Filter by minimum severity (default: warning)",
    "required": false
  },
  "source": {
    "type": "string",
    "description": "Filter by vassal name",
    "required": false
  },
  "limit": {
    "type": "integer",
    "description": "Max events to return (default: 20)",
    "required": false
  }
}
```

**Response**:
```json
{
  "events": [
    {
      "id": "evt-abc123",
      "source": "esp32-monitor",
      "severity": "error",
      "summary": "JSON deserialization error: unexpected field 'date_format'",
      "correlation": "Possibly related to commit a1b2c3d (changed date serialization in api/handler.go)",
      "created_at": "2026-04-02T10:30:15Z",
      "acknowledged": false
    }
  ]
}
```

---

## register_artifact

Register or update a file artifact in the Artifact Ledger.

**Parameters**:
```json
{
  "name": {
    "type": "string",
    "description": "Short artifact name (used in king://artifacts/{name})",
    "required": true
  },
  "file_path": {
    "type": "string",
    "description": "Absolute path to the artifact file",
    "required": true
  },
  "mime_type": {
    "type": "string",
    "description": "MIME type (auto-detected if omitted)",
    "required": false
  }
}
```

**Response**:
```json
{
  "name": "firmware.bin",
  "version": 2,
  "uri": "king://artifacts/firmware.bin",
  "checksum": "sha256:abc123..."
}
```

---

## resolve_artifact

Resolve a `king://artifacts/` reference to an actual file path.

**Parameters**:
```json
{
  "name": {
    "type": "string",
    "description": "Artifact name to resolve",
    "required": true
  }
}
```

**Response**:
```json
{
  "name": "firmware.bin",
  "file_path": "/home/user/project-hw/build/firmware.bin",
  "version": 2,
  "producer": "esp32-builder",
  "updated_at": "2026-04-02T10:25:00Z"
}
```

**Errors**:
- `ARTIFACT_NOT_FOUND`: No artifact registered with this name
