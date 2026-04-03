# Tasks: King v2.1 — The Diplomat & The Guard

**Input**: Design documents from `/specs/003-diplomat-guard/`
**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓

**Organization**: Tasks organized by user story to enable independent implementation and testing.

**Note on delegation (US1/US2)**: The core delegation system (`delegate_control`, `delegate_heartbeat`, `delegate_release`, warden loop) is **already implemented**. US1 work is limited to adding circuit breaker enforcement. US2 is fully done — warden.go handles 30s auto-recovery.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1–US4)

---

## Phase 1: Setup

**Purpose**: No new project structure needed — this is an existing Go project. Tasks here prepare the guard system scaffolding.

- [X] T001 Read and understand `internal/daemon/daemon.go` Daemon struct — identify exact insertion points for `guardStates` map and `guardStatesMu` mutex (no code changes yet)
- [X] T002 Read `internal/pty/session.go` and `internal/pty/manager.go` to understand how to: (a) read recent PTY output for `log_watch`, (b) track byte throughput for `data_rate`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Config layer and daemon state — MUST be complete before any guard implementation can start.

**⚠️ CRITICAL**: No user story guard work can begin until this phase is complete.

- [X] T003 Add `GuardConfig` struct to `internal/config/types.go` with fields: `Type`, `Interval`, `Threshold`, `Port`, `Expect`, `FailOn`, `Min`, `Exec`, `Timeout` (see data-model.md for exact spec)
- [X] T004 Add `Guards []GuardConfig` field to `VassalConfig` struct in `internal/config/types.go`
- [X] T005 Add guard validation to `Validate()` in `internal/config/config.go`: validate type is one of four valid values; validate required fields per type; validate `min` format (e.g. `"100bps"`); compile `fail_on` patterns as regexp; validate port range 1–65535; validate interval/threshold ≥ 1
- [X] T006 Add `GuardState` and `GuardResult` structs to `internal/daemon/daemon.go` (fields per data-model.md: `VassalName`, `GuardIndex`, `GuardType`, `ConsecutiveFails`, `LastCheckTime`, `LastResult`, `CircuitOpen`)
- [X] T007 Add `guardStates map[string]*GuardState` and `guardStatesMu sync.RWMutex` fields to the `Daemon` struct in `internal/daemon/daemon.go`; initialize the map in the constructor/`newDaemon` function

**Checkpoint**: Config parses `guards:` from `kingdom.yml` and daemon struct holds guard state — guard runner can now be implemented.

---

## Phase 3: User Story 1 — Multi-Agent Delegation (Circuit Breaker Enforcement) (Priority: P1) 🎯 MVP

**Goal**: Block AI sessions from taking delegation control when a circuit breaker is open for that vassal.

**Independent Test**: Configure a vassal with `threshold: 1` guard, trigger one failure, then call `delegate_control` for that vassal and verify it's rejected with a clear error message.

### Implementation for User Story 1

- [X] T008 [US1] Add helper method `anyCircuitOpen(vassal string) (bool, string)` to `internal/daemon/daemon.go` — reads `guardStates` under `RLock`, returns true + descriptive error string if any guard for the vassal has `CircuitOpen == true`
- [X] T009 [US1] Modify `delegate_control` handler in `internal/daemon/delegation_handlers.go` — call `d.anyCircuitOpen(req.Vassal)` before granting delegation; if circuit open, return error: `"Guard '<type>' (index <N>) circuit open for vassal '<name>'. Consecutive failures: <N>. AI modifications blocked."` (see contracts/mcp-tools.md for exact format)
- [X] T010 [US1] Add unit test in `internal/daemon/delegation_rpc_test.go`: (a) `TestDelegateControl_CircuitOpen` — set `CircuitOpen=true` in `guardStates`, call handler, assert error returned; (b) `TestDelegateControl_CircuitClosed` — verify delegation granted when circuit is closed

**Checkpoint**: `delegate_control` is now blocked by open circuit breakers. US1 is independently testable.

---

## Phase 4: User Story 3 — Hallucination Block via Guard (Priority: P2)

**Goal**: Guards run independently and detect real-world failures; circuit breaker accumulates consecutive failures and opens after threshold.

**Independent Test**: Add a `health_check` guard with `exec: ./scripts/fail.sh` (always exits 1) and `threshold: 2`, start daemon, wait 2 check intervals, verify `guardStates` has `ConsecutiveFails=2` and `CircuitOpen=true`, and verify `delegate_control` is blocked.

### Implementation for User Story 3

- [X] T011 [US3] Create `internal/daemon/guard_runner.go` — add `startGuardRunners(ctx context.Context)` function that: iterates all vassals in loaded config, spawns one goroutine per guard entry, each goroutine runs a ticker loop calling the appropriate check function, updates `GuardState`, and emits structured log event on circuit open/close
- [X] T012 [P] [US3] Implement `checkPortOpen(port int, expectOpen bool) GuardResult` in `internal/daemon/guard_runner.go` — attempt TCP dial to `127.0.0.1:<port>` with 2s timeout; return `ok=true` if connection succeeds (for `expect: open`) or fails (for `expect: closed`)
- [X] T013 [P] [US3] Implement `checkLogWatch(vassalName string, patterns []*regexp.Regexp, d *Daemon) GuardResult` in `internal/daemon/guard_runner.go` — read recent output lines from PTY manager (last `interval` seconds of output); return `ok=false` if any pattern matches any line, with the matching line in the message
- [X] T014 [P] [US3] Implement `checkDataRate(vassalName string, minBytesPerSec float64, d *Daemon) GuardResult` in `internal/daemon/guard_runner.go` — sample PTY byte counter at start and end of interval; compute rate; return `ok=false` if rate < threshold
- [X] T015 [P] [US3] Implement `checkHealthScript(execPath string, timeout time.Duration, rootDir string) GuardResult` in `internal/daemon/guard_runner.go` — run `exec.Command(execPath)` with `cmd.Dir = rootDir`; use `context.WithTimeout`; return `ok=false` if exit code != 0 or timeout exceeded; include script stderr in message
- [X] T016 [US3] Implement circuit breaker state update in `internal/daemon/guard_runner.go` — shared function `updateGuardState(d *Daemon, key string, result GuardResult, threshold int)`: on fail increment `ConsecutiveFails`; if `ConsecutiveFails >= threshold` set `CircuitOpen=true` and call `d.logger.Warn("GUARD_CIRCUIT_OPEN", ...)` if state changed; on pass reset `ConsecutiveFails=0` and set `CircuitOpen=false`, log `GUARD_CIRCUIT_CLOSED` if it was open
- [X] T017 [US3] Wire `startGuardRunners(ctx)` call in the Daemon `Start()` or `Run()` method in `internal/daemon/daemon.go` — call after config is loaded and before the main event loop
- [X] T018 [US3] Add PTY instrumentation needed by guards: add `RecentOutputLines(since time.Time) []string` method to `internal/pty/session.go` for `log_watch`; add `BytesWritten() int64` atomic counter to `internal/pty/session.go` for `data_rate` (increment in write path)
- [X] T019 [US3] Add unit tests in `internal/daemon/guard_runner_test.go`: (a) `TestCheckPortOpen_Open` — start net.Listener, verify pass; (b) `TestCheckPortOpen_Closed` — no listener, verify fail; (c) `TestCheckHealthScript_Pass` — write `exit 0` script; (d) `TestCheckHealthScript_Fail` — write `exit 1` script; (e) `TestCircuitBreaker_Opens` — 3 consecutive fails → `CircuitOpen=true`; (f) `TestCircuitBreaker_Recovers` — open circuit + 1 pass → `CircuitOpen=false`

**Checkpoint**: Guards run autonomously, circuit breaker opens/closes, and AI modifications are blocked. US3 is independently testable.

---

## Phase 5: User Story 4 — Guard Configuration (Priority: P3)

**Goal**: Developer can configure guards in `kingdom.yml` with zero friction; the daemon validates configuration at startup with clear error messages.

**Independent Test**: Write a `kingdom.yml` with an invalid guard (`type: bogus`), run `king up`, verify daemon exits with a clear validation error. Then write a valid config with all four guard types and verify daemon starts without error.

### Implementation for User Story 4

- [X] T020 [P] [US4] Add integration test in `tests/unit/config_serial_test.go` or new `internal/config/guard_config_test.go`: test valid configs for all 4 guard types; test each validation error case from contracts/kingdom-yaml.md (invalid type, missing required field, bad port range, bad min format, empty fail_on, invalid regex)
- [X] T021 [P] [US4] Update the default `kingdom.yml` template string in `internal/config/config.go` (`LoadOrCreateConfig`) to include a commented-out guard example showing all 4 types
- [X] T022 [US4] Verify `log_watch` `fail_on` patterns are compiled to `[]*regexp.Regexp` during config validation (in T005) and stored on the `GuardConfig` — add `CompiledPatterns []*regexp.Regexp` unexported field or use a separate compiled config struct
- [X] T023 [US4] Parse `min` value (e.g. `"100bps"`, `"1.5kbps"`, `"1mbps"`) into `float64` bytes/sec in config validation (T005) — store parsed value in `GuardConfig.MinBytesPerSec float64` for use by guard runner

**Checkpoint**: All four guard types can be configured in YAML and validated at daemon startup. US4 is independently testable.

---

## Phase 6: User Story 2 — Automatic Control Recovery (Priority: P2)

**Status**: ✅ **Already fully implemented** in `internal/daemon/warden.go`.

**Verification task only**:

- [X] T024 [US2] Run existing warden tests to confirm they still pass after guard system changes: `go test ./internal/daemon/ -run TestWarden -v` — verify `TestWardenTick*` tests pass and 30s heartbeat timeout behavior is intact

---

## Phase 7: Guard Status MCP Tool (Cross-cutting)

**Goal**: AI sessions can query real-time guard health via `guard_status` MCP tool.

- [X] T025 Create `internal/daemon/guard_handlers.go` — implement `registerGuardHandlers(d *Daemon)` that adds `guard_status` to `d.handlers`; handler reads all `GuardState` entries (or filters by vassal name param); returns JSON array per contracts/mcp-tools.md format
- [X] T026 Register `guard_status` daemon handler in `internal/daemon/daemon.go` `registerRealHandlers()` call (same pattern as `registerDelegationHandlers`)
- [X] T027 Add `registerGuardStatus()` MCP tool in `internal/mcp/tools.go` — follows existing pattern from `registerDelegateControl()`; calls daemon RPC `guard_status` via `newMCPDaemonClient`; optional `vassal` string param
- [X] T028 Register `registerGuardStatus()` in the MCP server setup (same location as `registerDelegateControl`)

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T029 [P] Run `go vet ./...` and `go build ./...` to verify no compilation errors across all changes
- [X] T030 [P] Validate `quickstart.md` examples work end-to-end: create test kingdom.yml with a `port_check` guard, start king, verify guard_status output
- [X] T031 [P] Update `CLAUDE.md` active technologies section to mention Guards system (003-diplomat-guard feature)
- [X] T032 Add `guard_status` output to TUI health panel (`internal/tui/components/health.go`) — display per-vassal guard state with circuit open/closed indicator

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **Phase 2 (Foundational)**: Depends on Phase 1 — BLOCKS all guard work
- **Phase 3 (US1)**: Depends on Phase 2 (needs `guardStates` map from T007)
- **Phase 4 (US3)**: Depends on Phase 2; T013/T014 depend on T018 (PTY instrumentation)
- **Phase 5 (US4)**: Depends on Phase 2 (T003–T005 config already done); T022/T023 are extensions of T005
- **Phase 6 (US2)**: Independent — verify only
- **Phase 7 (MCP Tool)**: Depends on Phase 4 (guard state must exist to query)
- **Phase 8 (Polish)**: Depends on all phases complete

### User Story Dependencies

- **US1 (P1)**: Needs Phase 2 (guardStates map) → T003–T007, then T008–T010
- **US2 (P2)**: Independent — just run existing tests (T024)
- **US3 (P2)**: Needs Phase 2 + T018 (PTY instrumentation) → T011–T019
- **US4 (P3)**: Needs Phase 2 → T020–T023

### Within US3 Parallel Opportunities

After T011 (guard_runner.go scaffold) is created, all four check implementations run in parallel:
- T012 (`port_check`) — independent file section
- T013 (`log_watch`) — independent file section
- T014 (`data_rate`) — independent file section
- T015 (`health_check`) — independent file section

---

## Parallel Execution Examples

### Phase 2 Parallel (after T003 completes)

```
T004 (add Guards field to VassalConfig)        ← depends on T003
T006 (GuardState struct in daemon.go)          ← independent of T004
```

### Phase 4 Parallel (after T011 scaffold)

```
T012 checkPortOpen implementation
T013 checkLogWatch implementation
T014 checkDataRate implementation
T015 checkHealthScript implementation
```

### Phase 7 Parallel (after Phase 4)

```
T025 + T026 (daemon-side guard_status handler)
T027 + T028 (MCP tool)
```

---

## Implementation Strategy

### MVP (US1 Only — Circuit Breaker Enforcement)

1. Complete Phase 2 (T003–T007) — foundational
2. Complete Phase 3 (T008–T010) — US1 circuit breaker check
3. **STOP and validate**: Start daemon, manually set `CircuitOpen=true` in memory, call `delegate_control` → verify blocked
4. This delivers the core "AI can be blocked" capability

### Full v2.1 Delivery

1. Phase 2 → Phase 3 (US1) → Phase 4 (US3) → Phase 5 (US4) → Phase 6 verify → Phase 7 → Phase 8
2. After Phase 4: "Hallucination Block" scenario fully works (ESP32 demo)
3. After Phase 5: Config is user-friendly for open source community
4. After Phase 7: AI can self-diagnose via `guard_status` tool

---

## Notes

- [P] tasks = different files or independent sections, safe to run in parallel
- Commit after each checkpoint (end of each phase)
- PTY instrumentation (T018) is the highest-risk task — validate with existing PTY tests before building guard runner on top of it
- All new daemon state is in-memory; no SQLite migrations needed
- Log messages use structured slog: `d.logger.Warn("GUARD_CIRCUIT_OPEN", "vassal", name, "guard_type", gType, "fails", n)`
