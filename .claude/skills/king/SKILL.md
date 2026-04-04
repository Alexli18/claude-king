---
name: king
description: Work with King — an AI orchestration system that manages multiple Claude Code "vassal" agents across sub-repositories. Use this skill when working in a King kingdom (directory has .king/ folder or .mcp.json with king/king-vassal entries), managing vassal processes, dispatching tasks to sub-agents, debugging the King daemon, creating or configuring kingdoms, or developing across multiple sub-repositories in a King setup.
---

# King Orchestration Skill

King coordinates multiple Claude Code agents ("vassals") across sub-repositories. One "Sovereign" (you) orchestrates N vassals via MCP.

## Architecture

- **King daemon** (`king up --detach`) — background process managing vassals, monitoring, event log
- **MCP gateway** (`king mcp`) — exposes King tools to Claude over stdio; attaches to running daemon
- **king-vassal** (`king-vassal --stdio`) — per-repo Claude Code agent, connects to King daemon
- **kingdom.yml** — config in `.king/` directory

For detailed architecture: see [references/architecture.md](references/architecture.md)

## King MCP Tools (Sovereign → King)

| Tool | Purpose |
|------|---------|
| `list_vassals` | List all vassal processes and status |
| `exec_in(vassal, command)` | Execute command in vassal's PTY |
| `dispatch_task(vassal, task)` | Dispatch task to a Claude vassal → returns task_id |
| `get_task_status(vassal, task_id)` | Poll dispatched task status |
| `abort_task(vassal, task_id)` | Cancel running task |
| `get_events([severity, source, limit])` | Read kingdom event log |
| `read_neighbor(path[, max_lines])` | Read file from a neighbor vassal's repo |
| `register_artifact(name, file_path)` | Register output file as artifact |
| `resolve_artifact(name)` | Get artifact path and metadata |
| `get_audit_log(...)` | Detailed audit log with filters |
| `get_action_trace(trace_id)` | Full trace of an exec_in execution |
| `respond_approval(request_id, approved)` | Approve/deny pending sovereign approval |
| `get_serial_events(vassal, ...)` | Events from serial (hardware) vassal |

## Vassal MCP Tools (king-vassal → Claude in sub-repo)

Each `king-vassal --stdio` exposes these tools to Claude working in that sub-repo:

| Tool | Purpose |
|------|---------|
| `dispatch_task(task)` | Receive and execute task from Sovereign |
| `get_task_status(task_id)` | Check own task status |
| `abort_task(task_id)` | Self-abort |

## Common Workflows

### Check kingdom state
```
list_vassals                         → shows name, status, pid, repo_path
get_events(severity="error")         → recent errors
```

### Dispatch work to a vassal
```
dispatch_task(vassal="firmware", task="Run all unit tests and report results")
→ returns {task_id: "..."}
get_task_status(vassal="firmware", task_id="...") → poll until completed/failed
```

### Execute command in vassal PTY
```
exec_in(vassal="ros2", command="colcon build --symlink-install", timeout_seconds=120)
```

### Read file from another repo
```
read_neighbor(path="emwirs-esp32-firmware/src/main.cpp")
```

## Creating a Kingdom

See [references/kingdom-config.md](references/kingdom-config.md) for full config templates and `.mcp.json` examples.

Quick setup:
1. Create `.king/kingdom.yml` (or run `king init`)
2. Create `.mcp.json` with king + vassal entries
3. Run `king up --detach` — starts daemon + launches vassals
4. Open Claude Code in the kingdom root — reads `.mcp.json`, starts `king mcp` + `king-vassal --stdio` for each sub-repo

## Debugging

See [references/debugging.md](references/debugging.md) for troubleshooting guide.

Quick checks:
```bash
tail -50 /path/to/.king/daemon.log    # daemon logs
king list                              # registry overview
king status                            # kingdom status
```
