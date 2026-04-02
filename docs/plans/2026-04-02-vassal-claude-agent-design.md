# Design: Vassal-as-Claude-Agent (Bidirectional MCP)

**Date**: 2026-04-02
**Status**: Approved

## Problem

Current vassals are dumb shell sessions. King can only type text into them via PTY and read unstructured output. There is no way to dispatch a structured task to another Claude Code agent and receive a structured result.

## Solution: Variant A — Vassal-as-MCP-Server

Each Claude-type vassal runs a `king-vassal` sidecar process that:
- Exposes an MCP server to King (`dispatch_task`, `get_task_status`, `abort_task`)
- Launches Claude Code headless (`claude -p`) with task context
- Connects back to King daemon as a client (for `register_artifact`, `get_events`, etc.)

## Architecture

```
King (Claude Code)
  MCP tools: dispatch_task, get_task_status, abort_task
       │
       │ MCP client (one per vassal)
       ▼
king-vassal sidecar          king-vassal (remote SSH)
  MCP server                   MCP server (via SSH tunnel)
  Claude Code (headless)        Claude Code (headless)
       │
       │ MCP client
       ▼
  King Daemon (UDS socket)
  register_artifact, get_events, read_neighbor
```

## Vassal Config (`kingdom.yml`)

```yaml
vassals:
  - name: frontend
    type: claude          # new field, default: "shell"
    repo_path: ./frontend
    autostart: true

  - name: ml-trainer
    type: claude
    repo_path: ./ml
    host: gpu-server      # SSH vassal (phase 2)
    ssh_user: alex
    autostart: true
```

## New MCP Tools (King side)

### `dispatch_task`
```json
{
  "vassal": "frontend",
  "task": "Add login button to Header.tsx",
  "context": {
    "artifacts": ["api_schema"],
    "notes": "Use shadcn/ui Button"
  }
}
→ { "task_id": "t-abc123", "status": "accepted" }
```

### `get_task_status`
```json
{ "vassal": "frontend", "task_id": "t-abc123" }
→ { "status": "done", "output": "...", "artifacts": ["Header.tsx"] }
```

### `abort_task`
```json
{ "vassal": "frontend", "task_id": "t-abc123" }
→ { "status": "aborted" }
```

## `king-vassal` Internals

On `dispatch_task`:
1. Write `.king/tasks/<task_id>.json` with task + context
2. Generate `VASSAL.md` in repo_path:
   - Vassal role and name
   - Task description
   - Available artifacts (resolved paths)
   - How to report completion (`kingctl report-done`)
3. Launch: `claude -p "$(cat task.json)" --dangerously-skip-permissions --output-format json`
4. Parse stdout for result
5. Call `register_artifact` on King daemon for any produced artifacts
6. Update task status in `.king/tasks/<task_id>.json`

### `VASSAL.md` template
```markdown
# You are a Vassal: {name}

Your King has assigned you a task. Work autonomously, then report completion.

## Task
{task}

## Available artifacts from other vassals
{artifact list with resolved paths}

## When done
Call: kingctl report-done --task {task_id} --artifacts <file1> <file2>
```

## Task Lifecycle

| Status | Meaning |
|---|---|
| `accepted` | king-vassal received, not yet started |
| `running` | Claude Code is working |
| `done` | Completed successfully |
| `failed` | Claude returned error |
| `timeout` | Exceeded time limit (default: 10 min) |
| `aborted` | abort_task was called |

## Data Flow

```
King → dispatch_task() → king-vassal
king-vassal → writes VASSAL.md → launches claude -p
claude works autonomously
claude exits → king-vassal parses result
king-vassal → register_artifact() → King daemon
King → get_task_status() → king-vassal → { status: "done", artifacts: [...] }
```

## Files to Create/Modify

| File | Change |
|---|---|
| `cmd/king-vassal/main.go` | New binary: MCP server + Claude Code launcher |
| `internal/config/types.go` | Add `Type` field to VassalConfig ("shell" \| "claude") |
| `internal/daemon/kingdom.go` | Launch king-vassal for type="claude" vassals |
| `internal/daemon/vassal_client.go` | New: MCP client pool for connecting to vassals |
| `internal/mcp/tools.go` | Add dispatch_task, get_task_status, abort_task |
| `cmd/kingctl/main.go` | Add `report-done` subcommand |

## MVP Constraints

- One active task per vassal (queue = phase 2)
- Default timeout: 10 minutes (configurable per vassal)
- SSH vassals: phase 2 (localhost only in MVP)
- No streaming task output (poll via get_task_status)
