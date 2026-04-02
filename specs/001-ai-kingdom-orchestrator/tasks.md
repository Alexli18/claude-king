# Tasks: Claude King — AI Kingdom Orchestrator

**Input**: Design documents from `/specs/001-ai-kingdom-orchestrator/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Not explicitly requested in spec — test tasks omitted. Add with TDD approach if needed.

**Organization**: Tasks grouped by user story for independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story (US1–US6)
- All paths relative to repository root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Go project initialization, directory structure, dependency installation

- [X] T001 Create project directory structure per plan: `cmd/king/`, `cmd/kingctl/`, `internal/daemon/`, `internal/pty/`, `internal/mcp/`, `internal/events/`, `internal/artifacts/`, `internal/store/`, `internal/config/`, `internal/tui/`, `tests/integration/`
- [X] T002 Initialize Go module (`go mod init`) and add core dependencies: `creack/pty` v2, `mark3labs/mcp-go`, `modernc.org/sqlite`, `charmbracelet/bubbletea` v1, `gopkg.in/yaml.v3`, `google/uuid`
- [X] T003 [P] Create `.king/kingdom.yml` JSON schema types in `internal/config/types.go` (KingdomConfig, VassalConfig, PatternConfig structs)
- [X] T004 [P] Create VMP protocol types in `internal/config/vassal_manifest.go` (VassalManifest, Skill, ArtifactDecl structs from `vassal.json` schema)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure shared by all user stories — daemon lifecycle, SQLite store, UDS communication, config loading

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Implement SQLite store initialization with schema migrations in `internal/store/db.go` (kingdoms, vassals, artifacts, events tables from data-model.md)
- [X] T006 Implement SQLite migrations runner in `internal/store/migrations.go` (embed SQL, auto-migrate on startup)
- [X] T007 [P] Implement `.king/kingdom.yml` config loader and validator in `internal/config/config.go` (parse YAML, defaults, validation)
- [X] T008 [P] Implement `vassal.json` VMP manifest loader in `internal/config/vassal_manifest.go` (parse JSON, merge skills/artifacts)
- [X] T009 Implement daemon lifecycle manager in `internal/daemon/daemon.go` (start, stop, signal handling, PID file at `.king/king.pid`, stale PID detection)
- [X] T010 Implement Unix Domain Socket server in `internal/daemon/daemon.go` (listen on `.king/king.sock`, accept connections, JSON-RPC dispatch)
- [X] T011 Implement Kingdom state manager in `internal/daemon/kingdom.go` (create/load/persist Kingdom entity via store, status transitions)

**Checkpoint**: Foundation ready — daemon can start, persist state, accept socket connections, load config

---

## Phase 3: User Story 1 — Launch a Kingdom Environment (Priority: P1) 🎯 MVP

**Goal**: `king up` creates PTY sessions from config and starts MCP server; `king down` tears everything down

**Independent Test**: Run `king up` in a directory with `.king/kingdom.yml` → verify daemon PID, socket file, PTY sessions created. Run `king down` → verify cleanup.

### Implementation for User Story 1

- [X] T012 [US1] Implement PTY session creation and management in `internal/pty/session.go` (start process in PTY via `creack/pty`, read/write, window resize, buffered output ring)
- [X] T013 [US1] Implement PTY session manager in `internal/pty/manager.go` (create/list/terminate sessions, track by name, persist to store)
- [X] T014 [US1] Implement `king up` command logic in `internal/daemon/kingdom.go` (load config → create vassals from VassalConfig → start autostart sessions → report readiness)
- [X] T015 [US1] Implement duplicate Kingdom detection in `internal/daemon/daemon.go` (check `.king/king.sock` exists and daemon responds → reconnect instead of duplicate)
- [X] T016 [US1] Implement default config generation in `internal/config/config.go` (when `.king/kingdom.yml` missing → create minimal default with shell session)
- [X] T017 [US1] Implement `king down` command logic in `internal/daemon/kingdom.go` (terminate all vassals → close socket → remove PID file → update store status)
- [X] T018 [US1] Implement `cmd/king/main.go` CLI entry point with `up` and `down` subcommands (parse args, invoke daemon)
- [X] T019 [US1] Implement basic MCP server setup in `internal/mcp/server.go` (initialize mcp-go server, register tools, start stdio transport)
- [X] T020 [US1] Implement `list_vassals` MCP tool in `internal/mcp/tools.go` (query session manager → return vassal list with status per contract)

**Checkpoint**: `king up` starts daemon + PTY sessions, `king down` stops. `list_vassals` returns session status via MCP.

---

## Phase 4: User Story 2 — Cross-Terminal Command Execution (Priority: P1)

**Goal**: Execute commands in any vassal from Claude Code via `exec_in` MCP tool, with output streaming

**Independent Test**: Launch Kingdom with 2+ vassals → call `exec_in("vassal-name", "echo hello")` via MCP → verify output returned. Test concurrent commands to same vassal → verify queuing.

### Implementation for User Story 2

- [X] T021 [US2] Implement command execution in PTY session in `internal/pty/session.go` (write command to PTY stdin, capture output until prompt or timeout, return result)
- [X] T022 [US2] Implement command queue per vassal in `internal/pty/session.go` (channel-based queue, sequential execution, queue position reporting)
- [X] T023 [US2] Implement `exec_in` MCP tool in `internal/mcp/tools.go` (resolve target vassal, dispatch command, return output/exit_code/duration per contract)
- [X] T024 [US2] Implement streaming output mode for `exec_in` in `internal/mcp/tools.go` (incremental text chunks for long-running commands)
- [X] T025 [US2] Implement timeout handling for `exec_in` in `internal/pty/session.go` (configurable timeout, graceful termination on timeout, TIMEOUT error)
- [X] T026 [US2] Implement `kingctl exec` CLI command in `cmd/kingctl/main.go` (connect to socket, send exec_in request, print output)
- [X] T027 [US2] Implement `kingctl status` and `kingctl list` CLI commands in `cmd/kingctl/main.go` (connect to socket, query status, formatted output)

**Checkpoint**: Commands can be executed in any vassal from Claude Code or kingctl. Concurrent commands are queued. Streaming works.

---

## Phase 5: User Story 3 — Cross-Terminal Event Awareness (Priority: P2)

**Goal**: Automatic error/event detection in vassal output, contextual alerts surfaced to King session

**Independent Test**: Configure error pattern → generate matching output in vassal → verify event created in store and retrievable via `get_events` MCP tool.

### Implementation for User Story 3

- [X] T028 [US3] Implement pattern matching engine in `internal/events/patterns.go` (compile regex patterns from config, match against output lines, extract groups)
- [X] T029 [US3] Implement Semantic Sieve output filter in `internal/events/sieve.go` (attach to PTY output stream, run pattern matching, deduplicate with cooldown, generate Event entities)
- [X] T030 [US3] Implement event bus in `internal/events/sieve.go` (publish detected events to subscribers: store, MCP notifications, TUI)
- [X] T031 [US3] Integrate Semantic Sieve with PTY session manager in `internal/pty/manager.go` (pipe session output through sieve before buffering)
- [X] T032 [US3] Implement event persistence in `internal/store/db.go` (insert events, query by severity/source/time, acknowledge events)
- [X] T033 [US3] Implement `get_events` MCP tool in `internal/mcp/tools.go` (query store with filters per contract: severity, source, limit)
- [X] T034 [US3] Implement context injection — system health summary generation in `internal/events/sieve.go` (aggregate recent events into brief status line for prompt injection)
- [X] T035 [US3] Implement `kingctl events` CLI command in `cmd/kingctl/main.go` (list recent events, filter by severity)

**Checkpoint**: Vassal output is monitored for patterns. Events are stored, retrievable, and surfaced to King. Noise reduction measurable.

---

## Phase 6: User Story 4 — File and Artifact Sharing (Priority: P2)

**Goal**: Register, version, and resolve artifacts between vassals via `king://artifacts/` references

**Independent Test**: Register artifact from vassal A → resolve `king://artifacts/name` from vassal B → verify correct file path returned.

### Implementation for User Story 4

- [X] T036 [P] [US4] Implement Artifact Ledger in `internal/artifacts/ledger.go` (register, update version, resolve by name, checksum verification via store)
- [X] T037 [P] [US4] Implement `king://artifacts/` URI resolver in `internal/artifacts/resolver.go` (parse URI scheme, resolve to file path, validate file exists)
- [X] T038 [US4] Implement `register_artifact` MCP tool in `internal/mcp/tools.go` (register file in ledger, auto-detect MIME type, return URI per contract)
- [X] T039 [US4] Implement `resolve_artifact` MCP tool in `internal/mcp/tools.go` (resolve name to path/version/producer per contract)
- [X] T040 [US4] Implement VMP auto-registration in `internal/daemon/kingdom.go` (on `king up`, parse `vassal.json` from repo_path, pre-register declared artifacts in ledger)
- [X] T041 [US4] Implement `read_neighbor` MCP tool in `internal/mcp/tools.go` (resolve path, validate permissions, read file content per contract)

**Checkpoint**: Artifacts can be registered, versioned, and resolved across vassals. VMP discovery works on startup.

---

## Phase 7: User Story 5 — Kingdom Lifecycle Management (Priority: P3)

**Goal**: Full isolation between Kingdoms, stale state cleanup, reliable `king down`

**Independent Test**: Launch two Kingdoms in different directories simultaneously → verify no shared state. Kill daemon → restart → verify clean recovery.

### Implementation for User Story 5

- [X] T042 [US5] Implement per-directory socket/PID isolation in `internal/daemon/daemon.go` (socket path derived from project root hash, no collision between Kingdoms)
- [X] T043 [US5] Implement stale state detection and cleanup in `internal/daemon/daemon.go` (on startup: check for orphaned `.king/king.sock`, stale PID → clean up and start fresh)
- [X] T044 [US5] Implement graceful shutdown with resource cleanup in `internal/daemon/kingdom.go` (terminate PTYs with SIGTERM → SIGKILL timeout, close DB, remove socket, update store)
- [X] T045 [US5] Implement crash recovery — vassal reattach on restart in `internal/pty/manager.go` (detect running processes from stored PIDs, reattach PTY if possible)
- [X] T046 [US5] Implement `kingctl logs` CLI command in `cmd/kingctl/main.go` (read vassal output buffer, tail mode)

**Checkpoint**: Multiple Kingdoms can run simultaneously without interference. Crash recovery works.

---

## Phase 8: User Story 6 — Interactive Dashboard (Priority: P3)

**Goal**: Bubbletea TUI showing vassal statuses, events, system metrics in real time

**Independent Test**: Launch TUI with running Kingdom → verify vassal list updates, events appear within 2s, vassal selection shows output.

### Implementation for User Story 6

- [X] T047 [US6] Implement TUI app scaffold in `internal/tui/app.go` (bubbletea program, alt-screen, base model with tab layout)
- [X] T048 [P] [US6] Implement vassal status table component in `internal/tui/components/vassals.go` (bubble-table with name, status, PID, last activity, real-time updates)
- [X] T049 [P] [US6] Implement event log component in `internal/tui/components/events.go` (scrollable list of recent events, color-coded by severity)
- [X] T050 [P] [US6] Implement system health component in `internal/tui/components/health.go` (Kingdom uptime, vassal count, event counts, memory usage)
- [X] T051 [US6] Implement vassal detail view in `internal/tui/components/detail.go` (select vassal → show recent output, execute commands from TUI)
- [X] T052 [US6] Integrate TUI with daemon via socket client in `internal/tui/app.go` (subscribe to status updates, events via UDS connection)
- [X] T053 [US6] Add `king dashboard` subcommand in `cmd/king/main.go` (launch TUI connected to running Kingdom)

**Checkpoint**: Full TUI dashboard showing real-time Kingdom state. Interactive vassal management.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Quality improvements across all user stories

- [X] T054 [P] Add structured logging throughout daemon with `log/slog` in `internal/daemon/daemon.go`
- [X] T055 [P] Implement log retention cleanup in `internal/store/db.go` (delete events older than `log_retention_days`)
- [X] T056 [P] Implement graceful degradation on disk full in `internal/store/db.go` (catch write errors, continue operating, alert user)
- [X] T057 [P] Implement PTY slot exhaustion handling in `internal/pty/manager.go` (detect ENOMEM/EAGAIN, clear error message, suggest closing sessions)
- [X] T058 [P] Implement vassal hang detection with configurable timeout in `internal/pty/session.go` (no output + no process exit for N seconds → warning event)
- [X] T059 Run quickstart.md validation — verify all commands in `specs/001-ai-kingdom-orchestrator/quickstart.md` work end-to-end
- [X] T060 Code cleanup: ensure all exported types have GoDoc comments, remove dead code, `go vet` clean

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **Phase 2 (Foundational)**: Depends on Phase 1 — BLOCKS all user stories
- **Phase 3 (US1 — Kingdom Launch)**: Depends on Phase 2
- **Phase 4 (US2 — Command Execution)**: Depends on Phase 3 (needs running Kingdom + PTY sessions)
- **Phase 5 (US3 — Event Awareness)**: Depends on Phase 3 (needs PTY output stream)
- **Phase 6 (US4 — Artifact Sharing)**: Depends on Phase 3 (needs running Kingdom), can parallel with Phase 4/5
- **Phase 7 (US5 — Lifecycle)**: Depends on Phase 3 (needs basic Kingdom)
- **Phase 8 (US6 — TUI)**: Depends on Phase 3, enhanced by Phases 4-7
- **Phase 9 (Polish)**: Depends on all desired user stories being complete

### User Story Dependencies

```
Phase 2 (Foundation) ──┬──→ US1 (P1) ──┬──→ US2 (P1)
                       │               ├──→ US3 (P2) ──→ US6 (P3, enhanced)
                       │               ├──→ US4 (P2)
                       │               └──→ US5 (P3)
                       └──→ (all stories require foundation)
```

- **US1**: Foundation only — no story dependencies
- **US2**: Requires US1 (needs PTY sessions to send commands to)
- **US3**: Requires US1 (needs PTY output to monitor)
- **US4**: Requires US1 (needs running Kingdom for ledger)
- **US5**: Requires US1 (needs basic Kingdom to add isolation)
- **US6**: Requires US1 (minimum), better after US3 (events to display)

### Parallel Opportunities Within Phases

- **Phase 1**: T003 ∥ T004
- **Phase 2**: T007 ∥ T008 (after T005-T006)
- **Phase 5**: T028 ∥ T029 (independent event components)
- **Phase 6**: T036 ∥ T037 (independent artifact components)
- **Phase 8**: T048 ∥ T049 ∥ T050 (independent TUI components)
- **Phase 9**: T054 ∥ T055 ∥ T056 ∥ T057 ∥ T058 (all independent)

---

## Parallel Example: User Story 1

```bash
# After Foundation (Phase 2) is complete:

# Launch PTY core components together:
Task: T012 "Implement PTY session in internal/pty/session.go"
Task: T016 "Implement default config generation in internal/config/config.go"

# After PTY session is ready, launch manager + daemon concurrently:
Task: T013 "Implement PTY manager in internal/pty/manager.go"
Task: T015 "Implement duplicate Kingdom detection in internal/daemon/daemon.go"
```

## Parallel Example: User Story 6

```bash
# After scaffold (T047) is ready:

# Launch all independent TUI components:
Task: T048 "Vassal status table in internal/tui/components/vassals.go"
Task: T049 "Event log component in internal/tui/components/events.go"
Task: T050 "System health component in internal/tui/components/health.go"
```

---

## Implementation Strategy

### MVP First (User Stories 1 + 2 Only)

1. Complete Phase 1: Setup (T001–T004)
2. Complete Phase 2: Foundational (T005–T011)
3. Complete Phase 3: User Story 1 — Kingdom Launch (T012–T020)
4. **STOP and VALIDATE**: `king up` → vassals created → `list_vassals` works
5. Complete Phase 4: User Story 2 — Command Execution (T021–T027)
6. **STOP and VALIDATE**: `exec_in` works → streaming works → `kingctl` works
7. Deploy MVP — core zero-switching value delivered

### Incremental Delivery

1. Setup + Foundation → base ready
2. US1 (Kingdom Launch) → `king up/down` works → **MVP v0.1**
3. US2 (Command Exec) → `exec_in` works → **MVP v0.2**
4. US3 (Event Awareness) → auto-alerting works → **v0.3**
5. US4 (Artifact Sharing) → `king://` URIs work → **v0.4**
6. US5 (Lifecycle) → multi-Kingdom isolation → **v0.5**
7. US6 (TUI Dashboard) → visual control center → **v1.0**

---

## Notes

- [P] tasks = different files, no dependencies
- [USn] maps to user story from spec.md
- All file paths use Go convention: `internal/` for private packages, `cmd/` for entry points
- Commit after each task or logical group
- Stop at any checkpoint to validate independently
