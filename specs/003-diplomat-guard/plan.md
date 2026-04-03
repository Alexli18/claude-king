# Implementation Plan: King v2.1 — The Diplomat & The Guard

**Branch**: `003-diplomat-guard` | **Date**: 2026-04-03 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/003-diplomat-guard/spec.md`

## Summary

Implement the **Guard system** for King v2.1. Delegation (The Diplomat) is already fully implemented — `delegate_control`, `delegate_heartbeat`, `delegate_release`, the 30-second warden loop, and the heartbeat goroutine all exist in the codebase. The net-new work is **The Guard**: runtime health checks that run independently of AI sessions, a circuit breaker that blocks AI modifications on consecutive failures, and the `guard_status` MCP tool. Guards are configured per-vassal in `kingdom.yml` with four check types: `port_check`, `log_watch`, `data_rate`, `health_check`.

## Technical Context

**Language/Version**: Go 1.22+
**Primary Dependencies**: mark3labs/mcp-go (MCP tools), creack/pty (PTY sessions), gopkg.in/yaml.v3 (config), charmbracelet/bubbletea (TUI health panel)
**Storage**: In-memory only (`sync.RWMutex`-protected map in Daemon struct); no DB writes
**Testing**: `go test ./...` — unit tests per package, existing `testhelpers_test.go` pattern
**Target Platform**: macOS + Linux (same as existing King)
**Project Type**: CLI daemon + MCP server
**Performance Goals**: Guard check overhead < 5% of daemon CPU at default 10s intervals
**Constraints**: No new import cycles; no new packages if avoidable; guard state resets on daemon restart by design
**Scale/Scope**: Typically 2–10 vassals per kingdom; 1–4 guards per vassal

## Constitution Check

Constitution file is a placeholder template — no project-specific gates defined. Proceeding with King-specific conventions:

- [x] No new import cycles (guard state in `Daemon` struct directly)
- [x] In-memory only — no SQLite writes for guard state
- [x] Existing MCP tool registration pattern followed
- [x] Existing `slog` logging pattern followed
- [x] No new packages required

## Project Structure

### Documentation (this feature)

```text
specs/003-diplomat-guard/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── mcp-tools.md     # MCP tool contracts
│   └── kingdom-yaml.md  # Configuration schema contract
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/config/
├── types.go             # ADD: GuardConfig struct + Guards field in VassalConfig
└── config.go            # ADD: guard validation in Validate()

internal/daemon/
├── daemon.go            # ADD: guardStates map + RWMutex; wire guard runner startup
├── delegation_handlers.go  # MODIFY: check circuit breaker before granting delegate_control
├── guard_runner.go      # NEW: guard runner goroutine + all 4 check type implementations
├── guard_handlers.go    # NEW: daemon-side guard_status RPC handler
└── warden.go            # UNCHANGED

internal/mcp/
├── tools.go             # ADD: register guard_status MCP tool
└── tools_delegation.go  # UNCHANGED (heartbeat already blocks on circuit open via daemon)

tests/unit/
└── guard_runner_test.go # NEW: unit tests for each guard type + circuit breaker logic
```

## Implementation Phases

### Phase 1: Config Layer

**Goal**: Extend `VassalConfig` with `Guards []GuardConfig` and validate them.

Files:
- `internal/config/types.go`: Add `GuardConfig` struct and `Guards` field
- `internal/config/config.go`: Add guard validation in `Validate()`

Key decisions:
- `GuardConfig` lives in `internal/config` (same as `VassalConfig`)
- Parse `min` (e.g. `"100bps"`) into bytes/sec at validation time, store as `float64` field
- `fail_on` patterns for `log_watch` are compiled to `*regexp.Regexp` at validation time

Tests: extend `internal/config/config_test.go` with guard config cases.

---

### Phase 2: Guard State + Daemon Wiring

**Goal**: Add guard state tracking to `Daemon` and start runner goroutines.

Files:
- `internal/daemon/daemon.go`:
  - Add `guardStates map[string]*GuardState` + `guardStatesMu sync.RWMutex`
  - Add `GuardState` and `GuardResult` struct definitions
  - Call `d.startGuardRunners()` from `Start()` (after config is loaded)
- `internal/daemon/guard_runner.go` (new file):
  - `startGuardRunners()`: iterate vassals, spawn one goroutine per guard
  - Each goroutine: ticker loop → call appropriate check function → update state → check circuit breaker threshold → emit notification if CB fires
  - `checkPortOpen(port int) GuardResult`
  - `checkLogWatch(vassalName string, patterns []*regexp.Regexp) GuardResult`
  - `checkDataRate(vassalName string, minBytesPerSec float64) GuardResult`
  - `checkHealthScript(exec string, timeout time.Duration, rootDir string) GuardResult`

Key decisions:
- Log watch reads from `d.ptyManager.RecentOutput(vassalName, duration)` — needs `RecentOutput` method on PTY manager (or equivalent)
- Data rate reads byte counters from PTY session (add `BytesWritten()` counter to `pty.Session`)
- Circuit breaker notification: `d.logger.Warn("GUARD_CIRCUIT_OPEN", ...)` — TUI health panel already subscribes to structured log events
- Circuit breaker auto-recovery: on next passing check, `consecutive_fails = 0`, `circuit_open = false`, log `GUARD_CIRCUIT_CLOSED`

---

### Phase 3: Enforcement + MCP Tool

**Goal**: Block AI modifications when circuit open; expose `guard_status` to AI.

Files:
- `internal/daemon/delegation_handlers.go`:
  - In `delegate_control` handler: before granting, call `d.anyCircuitOpen(vassalName)` → return error if true
- `internal/daemon/guard_handlers.go` (new file):
  - `registerGuardHandlers(d *Daemon)` — adds `guard_status` to `d.handlers`
  - Returns JSON array of all `GuardState` entries (or filtered by vassal)
- `internal/mcp/tools.go`:
  - `registerGuardStatus()` — MCP tool wrapping daemon `guard_status` RPC

Key decisions:
- `anyCircuitOpen(vassal)` reads `guardStates` under `RLock` — non-blocking
- Error message format: `"Guard 'log_watch' (index 1) circuit open for vassal '%s'. Consecutive failures: %d. AI modifications blocked."`
- `guard_status` MCP tool calls `guard_status` daemon RPC via `newMCPDaemonClient` (same pattern as existing delegation tools)

---

### Phase 4: Tests

**Goal**: Full coverage of guard logic.

Files:
- `internal/daemon/guard_runner_test.go` (or pattern from existing test files):
  - Circuit breaker: N-1 failures → circuit closed; N failures → circuit open; pass → auto-recover
  - `port_check`: mock listener on free port
  - `data_rate`: inject fake byte counters into PTY session
  - `health_check`: write temporary scripts that exit 0 / exit 1
  - `log_watch`: inject fake output lines into PTY buffer
- Extend `internal/daemon/delegation_rpc_test.go`:
  - `delegate_control` with open circuit → blocked
  - `delegate_control` with closed circuit → granted

## Complexity Tracking

No constitution violations. The only non-trivial complexity is the PTY instrumentation for `data_rate` and `log_watch` guards, justified by the requirement to monitor physical device output streams without adding disk I/O.
