# Design: King Daemon ↔ king-vassal Bidirectional Link

**Date**: 2026-04-02
**Status**: Approved

## Problem

king-vassal запускается daemon'ом и принимает задачи через MCP, но не имеет обратного канала: артефакты не регистрируются, завершение задач не сигнализируется, Auto-Trigger не работает. Связка односторонняя.

## Solution: Approach A + notify endpoint

Используем существующий `daemon.Client` (JSON-RPC по Unix socket) для канала vassal → daemon. Для канала daemon → vassal расширяем vassal MCP сервер новым инструментом `notify`. MCP Notifications (`SendNotificationToAllClients`) пушат события в King (Claude Code).

## Architecture

```
King (Claude Code)
  ↑ MCP Notification: kingdom/task_done
  │ (SendNotificationToAllClients)
  │
King Daemon (king-<hash>.sock)
  ├── RPC handlers:
  │     register_artifact  (Ledger.Register)
  │     read_neighbor      (Ledger.Resolve)
  │     task_done          (Auto-Trigger + MCP Notification)
  ├── ArtifactLedger
  ├── Auto-Trigger engine (on_task_done rules)
  └── VassalClientPool
        ├── dispatch_task → vassal   (existing)
        └── notify → vassal          (new)
              ↕
king-vassal sidecar
  ├── daemon.Client (connects to --king-sock at startup)
  │     After runTask:
  │       register_artifact(vassal, task_id, name, path)
  │       task_done(vassal, task_id, tag, artifacts, output)
  │     Before runTask:
  │       read_neighbor(artifact_name) → path
  │
  └── MCP server + new tool:
        notify(type, payload)
          "task_cancelled" → call s.taskCancel()
          "context_update" → log dependency changed
          "priority_boost" → future use
```

## Data Flow Example

```
backend completes "Generate OpenAPI spec" (tag: api)
  → king-vassal: register_artifact("openapi.json", "/backend/openapi.json")
  → king-vassal: task_done(task_id, tag="api", artifacts=["openapi.json"])
  → daemon: stores in Ledger
  → daemon: finds rule on_task_done[from=backend, tag=api]
  → daemon: dispatch_task(vassal=frontend, task="Update API types")
  → daemon: notify(vassal=frontend, type="context_update")
  → daemon: SendNotificationToAllClients("kingdom/task_done", {...})
  → King sees: «backend[api] done → frontend dispatched»
```

## kingdom.yml Configuration

```yaml
on_task_done:
  - from: backend
    tag: api
    notify: [frontend]
    dispatch:
      vassal: frontend
      task: "Update API types — backend registered new openapi.json"

  - from: ml
    tag: model_ready
    notify: [hardware, frontend]
```

## New Config Types

```go
type OnTaskDoneRule struct {
    From     string   `yaml:"from"`
    Tag      string   `yaml:"tag"`
    Notify   []string `yaml:"notify,omitempty"`
    Dispatch *struct {
        Vassal string `yaml:"vassal"`
        Task   string `yaml:"task"`
    } `yaml:"dispatch,omitempty"`
}

// Added to KingdomConfig:
OnTaskDone []OnTaskDoneRule `yaml:"on_task_done,omitempty"`
```

## Task struct extension

Add `Tag string` field to `internal/vassal/task.go`:

```go
type Task struct {
    // ... existing fields
    Tag string `json:"tag,omitempty"`
}
```

`dispatch_task` MCP tool accepts optional `tag` parameter.

## RPC Handlers (daemon side)

### `register_artifact`
```
params: {vassal, task_id, name, path}
action: ledger.Register(kingdomID, name, path, metadata{vassal, task_id})
return: {ok: true}
```

### `read_neighbor`
```
params: {name}
action: ledger.Resolve(kingdomID, name)
return: {path: "/abs/path/to/file"}
```

### `task_done` (new)
```
params: {vassal, task_id, tag, artifacts[], output}
action:
  1. Update .king/tasks/<id>.json status=done
  2. Run Auto-Trigger engine
  3. SendNotificationToAllClients("kingdom/task_done", payload)
return: {ok: true}
```

## MCP Notification (daemon → King)

```go
func (s *Server) NotifyTaskDone(vassal, taskID, tag string, artifacts []string, summary string) {
    s.mcpServer.SendNotificationToAllClients("kingdom/task_done", map[string]any{
        "vassal":    vassal,
        "task_id":   taskID,
        "tag":       tag,
        "artifacts": artifacts,
        "summary":   summary,
    })
}
```

## `notify` tool (daemon → king-vassal)

New MCP tool registered in `internal/vassal/server.go`:

```go
mcp.NewTool("notify",
    mcp.WithDescription("Receive a notification from King daemon"),
    mcp.WithString("type", mcp.Required()),
    mcp.WithObject("payload"),
)
```

Handler behaviour:
- `"task_cancelled"` → call `s.taskCancel()` to kill subprocess
- `"context_update"` → log that a dependency artifact was updated
- others → log and ignore

## Files to Create/Modify

| File | Change |
|---|---|
| `internal/config/types.go` | Add `OnTaskDoneRule`, `OnTaskDone []OnTaskDoneRule` to KingdomConfig |
| `internal/vassal/task.go` | Add `Tag string` field |
| `internal/vassal/server.go` | Accept `*daemon.Client`; call register_artifact+task_done after runTask; call read_neighbor before runTask; add `notify` MCP tool |
| `cmd/king-vassal/main.go` | Create `daemon.Client` from `--king-sock`; pass to VassalServer |
| `internal/daemon/daemon.go` | Implement `register_artifact`, `read_neighbor`, `task_done` handlers; Auto-Trigger engine |
| `internal/mcp/server.go` | Add `NotifyTaskDone` method using SendNotificationToAllClients |

## MVP Constraints

- `tag` is optional; rules without tag match all tasks from that vassal
- Auto-Trigger fires at most once per task_done (no loops/cycles detection needed in MVP)
- `notify` tool on vassal: only `task_cancelled` has functional effect in MVP
- `read_neighbor` is called only for artifacts listed in dispatch context (not auto-resolved)
