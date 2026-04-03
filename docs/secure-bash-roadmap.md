# Secure Bash Execution Roadmap

A practical hardening plan for shell command execution in Claude King's agentic workflow context.

**Status:** Design document. None of the Phase 2 or Phase 3 items are implemented.

---

## Goals

- Reduce the blast radius of AI model misbehavior when executing shell commands
- Provide operators with visibility into what commands are being run and why
- Make command execution progressively more restrictive as the operator's confidence grows
- Preserve the core utility of `exec_in` for legitimate agentic workflows

## Non-Goals

- Preventing a determined malicious actor with OS-level access from causing harm
- Full OS-level sandboxing as a first release feature
- Replacing `exec_in` with a domain-specific scripting language

---

## Threat Model

The primary threats in agentic command execution are not external attackers — they are:

1. **AI model prompt injection:** an adversarial input causes the model to emit a harmful command
2. **Model misbehavior under confusion:** a model misinterprets a task and runs destructive commands
3. **Vassal compromise via toolchain vulnerability:** a build tool or dependency runs unexpected commands
4. **Orchestrator mistake:** the King orchestrator passes a command to the wrong vassal

The daemon (`king up`) runs with the full privileges of the invoking user. There is no uid separation between King and vassals.

---

## Trust Boundaries

```
User (Sovereign)
  └── King daemon (trusted, full user privileges)
        └── Vassal process (trusted, same user)
              └── PTY session (exec_in target)
                    └── Shell / commands (untrusted inputs from AI)
```

The current enforcement gap is between "Shell / commands" and the trust granted to the vassal. Commands entered via `exec_in` are treated as fully trusted at the OS level.

---

## High-Risk Command Categories

| Category | Examples | Risk |
|----------|---------|------|
| Destructive filesystem | `rm -rf`, `rmdir`, `truncate`, `shred` | Data loss |
| Credential exfiltration | `cat ~/.aws/*`, `env`, `printenv`, `export` | Secret leakage |
| Network exfiltration | `curl <url>`, `wget <url>`, `nc` | Data exfiltration |
| Piped execution | `curl ... \| bash`, `wget ... \| sh` | Remote code execution |
| Force git operations | `git push --force`, `git reset --hard` | History rewrite |
| Package installation | `npm install`, `pip install`, `go get` | Supply chain |
| Privilege escalation | `sudo`, `su`, `chmod 777` | Privilege escalation |

---

## Implementation Strategies

### Option A: Allowlist / Denylist (Recommended for Phase 2)

Maintain a configurable list of allowed and denied command patterns. King evaluates the command string before passing it to the PTY.

**Pros:** simple to implement, auditable, incrementally tightened
**Cons:** pattern matching is bypassable (e.g. `b''a''sh` obfuscation), requires ongoing maintenance

### Option B: Wrapper Process

Insert a wrapper between `exec_in` and the PTY that logs every command, applies filters, and optionally waits for approval before forwarding to the shell.

**Pros:** transparent to the shell, captures real subprocess behavior
**Cons:** more complex to implement correctly; cannot intercept commands sent directly to PTY stdin

### Option C: Dry-Run / Plan-First Mode

Before executing a command, require the AI to emit a structured plan: `{"action": "delete", "path": "dist/", "reason": "clean build"}`. King validates the plan against a schema and optionally requires human approval before execution.

**Pros:** structured, auditable, human-reviewable
**Cons:** requires AI model cooperation; a confused model may not emit well-formed plans

### Option D: Policy-Based Approvals

Extend the sovereign approval mechanism to cover all `exec_in` calls by default, with an allowlist of low-risk patterns that bypass approval (e.g. `go test ./...`, `make build`).

**Pros:** leverages existing approval infrastructure (`internal/audit/approval.go`)
**Cons:** approval fatigue; slows down legitimate workflows

### Option E: Containerized Execution

Run vassal PTY sessions inside lightweight containers (Docker, nsjail) with network isolation and filesystem restrictions.

**Pros:** strong isolation; prevents exfiltration via network
**Cons:** significant infrastructure requirement; breaks workflows that require host filesystem access

---

## Phased Rollout Plan

### Phase 1: Audit Logging (Current gap to close)

- Add structured logging for every `exec_in` call: vassal, command, exit code, duration
- Store log entries in SQLite alongside artifact records
- Surface recent `exec_in` history in `king status` and the TUI

No behavioral change. Pure observability. Low risk.

**Implementation touch points:** `internal/mcp/tools.go` (`handleExecIn`), `internal/store/db.go`

### Phase 2: Denylist for Highest-Risk Patterns

- Add a configurable `exec_policy` section to `kingdom.yml`
- Default: block patterns matching `curl.*\|.*sh`, `wget.*\|.*sh`, `rm -rf /`
- Log and reject blocked commands with a structured error: `EXEC_BLOCKED: matched denylist pattern "..."`
- Allow operators to extend or disable the denylist

**Implementation touch points:** new `internal/policy/` package, `internal/mcp/tools.go` (`handleExecIn`)

### Phase 3: Optional Sandboxing

- Add a `sandbox: docker` option to vassal config
- When enabled, vassal PTY sessions run inside a named Docker container with:
  - Read-only bind mount of the repo at `/workspace`
  - No network access by default (configurable)
  - Ephemeral writable layer discarded after session
- Integrate with existing PTY manager (`internal/pty/`)

This is an advanced feature and should not be attempted before Phase 1 and 2 are stable.

---

## Audit Logging Requirements

Every `exec_in` call should produce a structured log entry with:

```json
{
  "event": "exec_in",
  "vassal": "my-agent",
  "command": "go test ./...",
  "exit_code": 0,
  "duration_ms": 1240,
  "approved_by": "sovereign",
  "timestamp": "2026-04-04T12:00:00Z"
}
```

Entries should be queryable via `king status --exec-history` and the `get_events` MCP tool.
