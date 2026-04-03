# Permission Model Review

## Summary

Claude King's permission model consists of two partially overlapping mechanisms: the sovereign `ApprovalManager` (request/respond pattern for `exec_in` routed through the daemon) and the guard circuit breaker (blocks `delegate_control` when health checks fail). Neither mechanism covers the full set of state-modifying operations available through MCP tools.

## Scope

- `exec_in` approval flow
- `delegate_control` enforcement
- MCP tool authorization surface

## Relevant Components

- `internal/audit/approval.go` — `ApprovalManager`: manages pending approval channels by request ID; `Request()` returns a channel that blocks until `Respond()` is called
- `internal/daemon/daemon.go` — sovereign approval gate (around line 1250): reads `d.config.Settings.SovereignApproval`, creates `ApprovalRequest` in store, and blocks via `ApprovalManager.Request()`
- `internal/daemon/delegation_handlers.go` — `anyCircuitOpen()`: reads guard state before allowing delegation; returns an error message if any guard circuit is open for the target vassal
- `internal/mcp/tools.go` — defines all MCP tool handlers; `handleExecIn` invokes `session.ExecCommand` directly (approval is enforced at the daemon RPC layer, not here); only `handleRespondApproval` interacts with `ApprovalManager` directly in the MCP layer
- `internal/mcp/server.go` — `RegisterTools()`: registers all tools; no per-tool permission levels defined at registration time

## Risk Description

Of the MCP tools registered in `internal/mcp/server.go`, only `exec_in` has a sovereign approval gate, and that gate is enforced in the daemon RPC handler rather than in the MCP tool handler itself. All other tools execute without any approval gate, including:

- **Read operations:** `list_vassals`, `get_events`, `guard_status`, `get_audit_log`, `get_action_trace`, `get_serial_events`, `delegate_status`
- **State-modifying:** `register_artifact`, `dispatch_task`, `abort_task`, `delegate_release`
- **Read with sensitive data:** `get_audit_log` and `get_action_trace` expose command history and approval records to any MCP caller without additional authentication

Operations like `register_artifact` and `dispatch_task` can have significant side effects: storing potentially sensitive data to the Ledger, spawning new vassal processes, modifying kingdom state. These are not gated by either the approval mechanism or the circuit breaker.

## Abuse Scenario

An AI model calls `dispatch_task(vassal="finance-agent", task="Send wire transfer confirmation to external-webhook.com")` without any approval required. The task is dispatched and executed by the vassal. No Sovereign is notified.

## Existing Safeguards

- **Sovereign approval gate** gates `exec_in` when configured in `SovereignApproval`. This is the most direct enforcement for command execution.
- **Artifact scanning** (`internal/security/scanner.go`) blocks secret-containing files from the Ledger via `Ledger.Register()` in `internal/artifacts/ledger.go`.
- **Guard circuit breaker** (`anyCircuitOpen`) prevents a degraded vassal from being assigned new delegations via `delegate_control`.
- **Audit trail** (`CreateActionTrace` in `internal/daemon/daemon.go`) records `exec_in` operations in SQLite for post-hoc review.

## Gaps

- No permission levels (e.g. read-only, modify, execute) defined for MCP tools at registration time in `RegisterTools()`.
- `ApprovalManager` is instantiated directly inside `daemon.Daemon` — it cannot be swapped for a different enforcement strategy without modifying `daemon.go`.
- No approval required for `dispatch_task`, `register_artifact`, or `abort_task`.
- The circuit breaker only protects `delegate_control`, not other state-modifying tools.
- The approval gate for `exec_in` is in the daemon RPC layer; if the MCP tool handler is called outside the daemon context, the gate does not apply.

## Recommendations

1. **Define risk tiers** for MCP tools: read-only (safe), state-modifying (medium), execution (high). Document which tier each tool falls into.
2. **Extend approval coverage** to at minimum `dispatch_task` and `register_artifact` in a future release.
3. **Extract a `PermissionGateway` interface** from `ApprovalManager` to decouple enforcement from `daemon.Daemon`. To be detailed in `docs/permission-model.md` (planned).
4. **Add TODO comments** at the registration points in `internal/mcp/server.go` marking tools that lack approval gates.
