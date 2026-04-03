# Permission Model

Documents the current approval and enforcement model in Claude King and proposes a refactoring path toward a reusable permission subsystem.

---

## Current Approval Flow

The only operation currently gated by explicit approval is `exec_in`. The flow is:

```
exec_in MCP call → daemon RPC handler (internal/daemon/daemon.go)
  │
  ├─ [sovereign_approval not configured] → executes immediately
  │
  └─ [sovereign_approval: true]
       │
       ├─ store.CreateApprovalRequest(req) → persists full ApprovalRequest to SQLite
       ├─ ApprovalManager.Request(approvalID) → returns buffered channel
       ├─ MCP tool respond_approval(request_id, approved) called by Sovereign
       │     └─ ApprovalManager.Respond(approvalID, approved)  (handleRespondApproval in tools.go)
       └─ daemon handler receives decision, proceeds or returns APPROVAL_REJECTED
```

Source: `internal/audit/approval.go` (ApprovalManager), `internal/daemon/daemon.go` (sovereign gate at line 1250), `internal/mcp/tools.go` (handleRespondApproval)

Note: The MCP-layer `handleExecIn` in `tools.go` does not itself contain approval logic. The approval gate lives exclusively in the daemon's internal RPC handler registered in `daemon.go`. The MCP server's `handleExecIn` is a separate code path used when running king-vassal in stdio MCP mode without a daemon.

The `ApprovalManager` is held as a concrete struct field (`approvalMgr *audit.ApprovalManager`) in `daemon.Daemon` (`internal/daemon/daemon.go`). The `mcp.Server` receives it behind an `ApprovalManager` interface defined in `internal/mcp/server.go`.

---

## Guard Circuit Breaker (Separate Enforcement Layer)

The circuit breaker is a second enforcement mechanism, independent of the approval flow:

```
delegate_control MCP call → daemon RPC handler (internal/daemon/delegation_handlers.go)
  │
  └─ anyCircuitOpen(vassal) checks d.guardStates map under RLock
       ├─ circuit open → returns error, delegation blocked
       └─ circuit closed → delegation proceeds
```

The circuit breaker only gates `delegate_control`. It does not affect `exec_in`, `dispatch_task`, or any other MCP tool.

Source: `internal/daemon/delegation_handlers.go` (`anyCircuitOpen`)

---

## Permission Levels (Not Yet Defined in Code)

The codebase does not define explicit permission tiers. Informally, MCP tools fall into these categories:

| Tier | Tools | Current enforcement |
|------|-------|-------------------|
| Read-only | `list_vassals`, `get_events`, `get_serial_events`, `guard_status`, `get_audit_log`, `get_action_trace`, `resolve_artifact`, `read_neighbor`, `delegate_status`, `get_task_status` | None required |
| State-modifying | `register_artifact`, `dispatch_task`, `abort_task`, `delegate_release`, `respond_approval` | None |
| Execution | `exec_in`, `delegate_control` | Partial (sovereign approval for exec_in via daemon handler; circuit breaker for delegate_control) |

Note: `get_audit_log` and `get_action_trace` expose command history and approval records to any MCP caller without additional authentication.

---

## Where the Design Is Tightly Coupled

1. **`ApprovalManager` as concrete type in `daemon.Daemon`:** `daemon.go` holds `approvalMgr *audit.ApprovalManager` as a concrete struct field. Swapping for a different enforcement strategy requires modifying `daemon.go`.

2. **Approval only for `exec_in` in daemon handler:** The sovereign approval gate exists only in the daemon's internal `exec_in` RPC handler closure. The MCP-layer `handleExecIn` in `tools.go` executes with no approval gate at all. Adding approval to other tools requires manually threading the logic into each additional handler.

3. **Circuit breaker embedded in `delegation_handlers.go`:** `anyCircuitOpen` reads `d.guardStates` directly under `d.guardStatesMu.RLock()`. It is not accessible as a general-purpose enforcement primitive.

4. **No unified enforcement point:** There is no middleware or interceptor layer where policies could be applied uniformly to all MCP tool calls.

---

## Refactoring Proposal

### Phase 1: Extract PermissionGateway interface (Low risk, high value)

Define an interface in a new package `internal/permission/`:

```go
// Gateway is the single enforcement point for permission decisions.
type Gateway interface {
    // RequestApproval blocks until the Sovereign approves or rejects requestID.
    // Returns true if approved, false if rejected or timed out.
    RequestApproval(ctx context.Context, requestID, description string) (bool, error)

    // CheckGuards returns an error if any guard circuit for vassal is open.
    CheckGuards(vassal string) error

    // CheckDelegation returns an error if vassal is already delegated and force is false.
    CheckDelegation(vassal string, force bool) error
}
```

The concrete implementation wraps `ApprovalManager` and the existing guard state reader. `daemon.Daemon` constructs the concrete type; `mcp.Server` receives the interface.

This decouples enforcement from both the daemon struct internals and the individual MCP tool handlers.

### Phase 2: Register tool risk tiers (No behavioral change)

Add metadata comments at tool registration in `internal/mcp/server.go` marking risk tiers. This documents extension points without changing behavior — see the TODO markers added in Part B of this task.

### Phase 3: Policy-based enforcement (Future)

Once the `Gateway` interface exists, add a `PolicyEngine` that evaluates each tool call against configured rules loaded from `kingdom.yml`:

```go
type PolicyEngine struct {
    gateway Gateway
    rules   []Rule
}
```

This enables operators to configure per-tool approval requirements without code changes.
