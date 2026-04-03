# Research: King v2.1 — The Diplomat & The Guard

## What's Already Implemented (Delegation)

The delegation system is **substantially complete** in the current codebase:

| Component | File | Status |
|-----------|------|--------|
| `delegate_control` RPC handler | `internal/daemon/delegation_handlers.go` | Done |
| `delegate_heartbeat` RPC handler | `internal/daemon/delegation_handlers.go` | Done |
| `delegate_release` RPC handler | `internal/daemon/delegation_handlers.go` | Done |
| Warden goroutine (30s expiry) | `internal/daemon/warden.go` | Done |
| In-memory `delegatedVassals` map | `internal/daemon/daemon.go` | Done |
| MCP `delegate_control` tool | `internal/mcp/tools_delegation.go` | Done |
| MCP `delegate_release` tool | `internal/mcp/tools_delegation.go` | Done |
| MCP `delegate_status` tool (local) | `internal/mcp/tools_delegation.go` | Done |
| Heartbeat goroutine (10s) on MCP side | `internal/mcp/tools_delegation.go` | Done |
| `force` override flag | `internal/daemon/delegation_handlers.go` | Done |

**Decision**: Delegation implementation is complete. v2.1 adds **Guards** as the net-new system.

## What's Missing (Guards — Net New)

| Component | Rationale |
|-----------|-----------|
| `GuardConfig` struct in `VassalConfig` | Guards are opt-in per vassal in `kingdom.yml` |
| Guard runner goroutine in daemon | Background loop checking all guards |
| Guard state + circuit breaker tracking | Per-guard consecutive failure counter |
| Guard types: port_check, log_watch, data_rate, health_check | Four check implementations |
| Blocking AI modifications when CB active | Enforcement in delegation handlers |
| MCP `guard_status` tool | AI-visible guard health query |
| User notification on circuit breaker fire | Log + structured event to TUI |

## Decision: Guard Configuration Location

- **Decision**: Guards are configured in `vassals[].guards[]` inside `.king/kingdom.yml` (existing config file)
- **Rationale**: Avoids proliferation of config files; keeps all vassal settings co-located
- **Alternatives considered**: Separate `guards.yml` file — rejected (extra file, harder discovery); inline per-command CLI flags — rejected (too verbose, not persistent)

## Decision: Circuit Breaker Default Threshold

- **Decision**: Default N = 3 consecutive failures
- **Rationale**: Low enough to catch real problems quickly; high enough to avoid noise from transient blips
- **Alternatives considered**: N=1 (too sensitive), N=5 (too slow to react for physical systems)

## Decision: Guard Check Interval

- **Decision**: Configurable per-guard with a global default of 10 seconds
- **Rationale**: 10s is conservative enough to avoid CPU waste; user can reduce per guard if needed
- **Alternatives considered**: Fixed 5s (too aggressive for custom scripts), fixed 30s (too slow to detect failures)

## Decision: Guard Failure Notification Channel

- **Decision**: Structured log entries (slog) + TUI health panel update + MCP `guard_status` response
- **Rationale**: No persistent notification store needed; TUI already has a health panel component; AI can poll `guard_status`
- **Alternatives considered**: SQLite event storage — deferred (complexity); OS desktop notifications — out of scope

## Decision: Where to Enforce Circuit Breaker

- **Decision**: Enforcement in the daemon-side `delegate_control` and task dispatch handlers; any handler that performs AI-driven modification checks guard state first
- **Rationale**: Centralized enforcement at the daemon layer means no AI client can bypass it
- **Alternatives considered**: MCP-layer enforcement — rejected (MCP server would need daemon state, creates coupling)

## Decision: data_rate Guard Measurement

- **Decision**: Measures bytes passing through the vassal's PTY output stream per interval period (not TCP network traffic)
- **Rationale**: Vassal output is already streamed through the PTY session; instrumenting this is zero-overhead. Serial/ESP32 data naturally flows through PTY stdout.
- **Alternatives considered**: `/proc/net` polling — rejected (Linux-only, too low-level); separate sniffer process — rejected (complexity)

## Decision: log_watch Implementation

- **Decision**: Pattern matching runs on a sliding window of recent PTY output lines (last N seconds); does not scan log files on disk
- **Rationale**: PTY output is already buffered in `pty.Session`; disk log scanning adds latency and disk I/O
- **Alternatives considered**: `tail -f` style file watcher — deferred (add when disk log persistence is added)

## Decision: health_check Script Execution

- **Decision**: Scripts are executed from the kingdom root directory with a 10-second timeout per execution
- **Rationale**: Relative paths work naturally; 10s timeout prevents runaway scripts from blocking the guard loop
- **Alternatives considered**: Configurable timeout — yes, make it configurable with 10s default

## Decision: Import Cycle Avoidance

- **Decision**: Guard state is stored in the `Daemon` struct directly (no new package); GuardConfig is in `internal/config`
- **Rationale**: Existing pattern — daemon already holds `delegatedVassals` directly. No new packages means no new import cycles.
- **Alternatives considered**: Separate `internal/guard` package — rejected (would need circular imports or interface wrappers like VassalPool)
