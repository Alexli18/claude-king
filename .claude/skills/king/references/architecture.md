# King Architecture

## Core Concepts

### Kingdom
A project root directory with `.king/` subdirectory. Has:
- `kingdom.yml` — config (name, vassals, patterns, settings)
- `king.db` — SQLite state (events, audit, tasks, PTY sessions)
- `daemon.log` — daemon logs
- `vassals/` — Unix sockets for vassal connections

### Vassal Types
| Type | Description | Required fields |
|------|-------------|-----------------|
| `shell` (default) | Shell process with PTY | `command` |
| `serial` | Serial device (ESP32, Arduino) | `serial_port` |
| `claude` | Claude Code sub-agent via king-vassal | `repo_path` |

### Process Architecture

```
Claude Code (FIX/) ← Sovereign
  ├── king mcp [stdio]          ← MCP gateway (king binary)
  │     └── connects to daemon socket → .king/king-<hash>.sock
  ├── king-vassal --stdio [firmware]   ← vassal MCP server
  ├── king-vassal --stdio [ml-pipeline]
  ├── king-vassal --stdio [ros2]
  └── king-vassal --stdio [gpr]

King Daemon (background)
  ├── .king/king-<hash>.sock    ← RPC socket
  ├── Manages vassal processes
  ├── Event monitoring (pattern matching)
  └── Auto-restart on crash
```

### Startup Sequence
1. `king up --detach` → starts daemon, creates socket, launches `type:claude` vassals
2. Claude Code opens FIX/ → reads `.mcp.json`
3. Claude Code starts `king mcp` → attaches to running daemon (or starts its own)
4. Claude Code starts `king-vassal --stdio --name <vassal>` for each sub-repo → each resolves `repo_path` from `kingdom.yml` and connects to daemon for registration

### MCP Flow: Sovereign dispatching to Vassal

```
Sovereign (Claude in FIX/)
  → dispatch_task(vassal="firmware", task="...")   [via king MCP tool]
  → King routes to firmware's king-vassal socket
  → king-vassal runs Claude Code headlessly
  → Claude Code executes task in emwirs-esp32-firmware/
  → Result returned via get_task_status
```

## Key Files

| Path | Description |
|------|-------------|
| `.king/kingdom.yml` | Kingdom configuration |
| `.king/king.db` | SQLite state database |
| `.king/daemon.log` | Daemon stdout/stderr |
| `.king/king-<hash>.sock` | Daemon RPC socket |
| `.king/king-<hash>.pid` | Daemon PID file |
| `.king/vassals/<name>.sock` | Per-vassal MCP socket |
| `.mcp.json` | MCP server config for Claude Code |
| `vassal.json` | Optional per-repo vassal manifest |

## Event System

Daemon watches all vassal output for pattern matches. Built-in pattern:
```yaml
patterns:
  - name: generic-error
    regex: '(?i)error|FAIL|panic:'
    severity: error
    summary_template: "Error detected in {vassal}: {match}"
```

Severity levels: `info`, `warning`, `error`, `critical`
