# Tasks: The Royal Audit

**Input**: Design documents from `/specs/002-royal-audit/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/audit-mcp-tools.md

**Tests**: Not explicitly requested — test tasks omitted.

**Organization**: Tasks grouped by user story (P1-P4) to enable independent implementation.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story (US1, US2, US3, US4)
- Exact file paths included

---

## Phase 1: Setup

**Purpose**: Extend config types and database schema for audit

- [X] T001 Add audit settings fields to Settings struct in internal/config/types.go (audit_ingestion, audit_retention_days, audit_ingestion_retention_days, sovereign_approval, sovereign_approval_timeout, audit_max_trace_output)
- [X] T002 Update DefaultSettings() to include audit defaults in internal/config/config.go
- [X] T003 Add migration v2 with audit_entries, action_traces, approval_requests tables in internal/store/migrations.go

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Store CRUD methods and core AuditRecorder — MUST complete before user stories

- [X] T004 Add AuditEntry struct to internal/store/db.go and implement CreateAuditEntry, CreateAuditEntryBatch methods
- [X] T005 [P] Add ActionTrace struct to internal/store/db.go and implement CreateActionTrace, GetActionTrace, UpdateActionTrace, ListActionTraces methods
- [X] T006 [P] Add ApprovalRequest struct to internal/store/db.go and implement CreateApprovalRequest, GetApprovalRequest, UpdateApprovalRequest, ListPendingApprovals methods
- [X] T007 Add ListAuditEntries method with filters (kingdom_id, layer, source, since, until, trace_id, limit) to internal/store/db.go
- [X] T008 Add DeleteOldAuditEntries method (separate retention for ingestion vs sieve/action) to internal/store/db.go
- [X] T009 Create AuditRecorder struct with Ingestion/Sieve/Action recording methods in internal/audit/recorder.go

**Checkpoint**: Foundation ready — store supports audit CRUD, recorder is available for wiring

---

## Phase 3: User Story 1 — Real-Time Audit Stream (Priority: P1) MVP

**Goal**: Three-layer audit stream (Ingestion, Sieve, Action) visible in TUI and CLI

**Independent Test**: Start daemon, run `echo ERROR` in vassal, verify Audit Hall shows ingestion + sieve + event entries

### Implementation for User Story 1

- [X] T010 [US1] Wire AuditRecorder into Daemon.Start() — create after Sieve, pass store + kingdom ID + config settings in internal/daemon/daemon.go
- [X] T011 [US1] Add Sieve layer audit hook: record match/suppress/no-match decisions in Sieve.ProcessLine() by calling AuditRecorder.RecordSieve() in internal/events/sieve.go
- [X] T012 [US1] Add Ingestion layer audit hook: record raw output lines (when audit_ingestion=true) via onOutput wrapper in internal/daemon/daemon.go startVassals()
- [X] T013 [US1] Create BatchWriter for buffered ingestion writes (batch 100 / 1sec flush) in internal/audit/batcher.go
- [X] T014 [US1] Wire BatchWriter into AuditRecorder.RecordIngestion() in internal/audit/recorder.go
- [X] T015 [US1] Add get_audit_log RPC handler in internal/daemon/daemon.go registerRealHandlers()
- [X] T016 [P] [US1] Add get_audit_log MCP tool registration and handler in internal/mcp/server.go and internal/mcp/tools.go
- [X] T017 [US1] Create AuditModel and AuditView TUI component (scrollable list, layer color-coding) in internal/tui/components/audit.go
- [X] T018 [US1] Add tabAudit (tab 4) to TUI app — tab bar, key binding "4", data fetch via get_audit_log RPC in internal/tui/app.go
- [X] T019 [US1] Add `kingctl audit` subcommand with --layer, --vassal, --limit flags in cmd/kingctl/main.go
- [X] T020 [US1] Wire audit retention cleanup (DeleteOldAuditEntries) into Daemon.Start() alongside existing DeleteOldEvents in internal/daemon/daemon.go

**Checkpoint**: Audit stream works end-to-end — visible in TUI (tab 4), queryable via CLI and MCP

---

## Phase 4: User Story 2 — Action Trace (Priority: P2)

**Goal**: Every exec_in gets a Trace ID with full trigger→context→execution chain

**Independent Test**: Call exec_in via MCP, then query get_action_trace with returned trace_id and verify trigger/context/execution fields

### Implementation for User Story 2

- [X] T021 [US2] Add TraceID field to CommandResult struct in internal/pty/session.go and generate UUID-8 in executeCommand()
- [X] T022 [US2] Record ActionTrace (status=running) before ExecCommand and update (completed/failed/timeout) after in exec_in RPC handler in internal/daemon/daemon.go
- [X] T023 [US2] Add AuditRecorder.RecordAction() method that creates AuditEntry (layer=action) linked to trace_id in internal/audit/recorder.go
- [X] T024 [US2] Add get_action_trace RPC handler in internal/daemon/daemon.go registerRealHandlers()
- [X] T025 [P] [US2] Add get_action_trace MCP tool registration and handler in internal/mcp/server.go and internal/mcp/tools.go
- [X] T026 [US2] Add `kingctl audit --trace <id>` flag to show detailed Action Trace in cmd/kingctl/main.go
- [X] T027 [US2] Include trigger_event_id linking — when exec_in is called after a Sieve event, pass last event ID as trigger in internal/daemon/daemon.go

**Checkpoint**: All exec_in calls are traced with Trace ID, viewable via CLI/MCP, linked to trigger events

---

## Phase 5: User Story 3 — Time-Travel Debugging (Priority: P3)

**Goal**: Filter audit by time range, get system snapshot at a point in time

**Independent Test**: Generate events over 5 minutes, query `kingctl audit --since 2m --until 1m` and verify only entries from that window are returned

### Implementation for User Story 3

- [X] T028 [US3] Add --since and --until flags to `kingctl audit` with RFC3339 and relative time parsing (5m, 1h, 1d) in cmd/kingctl/main.go
- [X] T029 [US3] Add since/until time range filter support to get_audit_log RPC handler in internal/daemon/daemon.go
- [X] T030 [US3] Add since/until parameters to get_audit_log MCP tool in internal/mcp/server.go and internal/mcp/tools.go
- [X] T031 [US3] Implement relative time parsing helper (parseRelativeTime) in internal/audit/recorder.go
- [X] T032 [US3] Add TUI Audit Hall filter mode — press 'f' to enter filter, type since/until, press Enter to apply in internal/tui/components/audit.go
- [X] T033 [US3] Add `kingctl snapshot <time>` subcommand — shows active vassals, recent events, recent actions at given timestamp in cmd/kingctl/main.go

**Checkpoint**: Time-travel filtering works in CLI/MCP/TUI, snapshot provides system state at any point

---

## Phase 6: User Story 4 — Sovereign Approval (Priority: P4)

**Goal**: exec_in commands require explicit user approval when sovereign_approval=true

**Independent Test**: Enable sovereign_approval in config, call exec_in via MCP, verify command blocks until `kingctl approve <id>` is run

### Implementation for User Story 4

- [X] T034 [US4] Create ApprovalManager with pending channel, Wait/Respond methods in internal/audit/approval.go
- [X] T035 [US4] Wire ApprovalManager into Daemon — create in Start(), pass to exec_in handler in internal/daemon/daemon.go
- [X] T036 [US4] Add approval gate in exec_in RPC handler — if sovereign_approval enabled, create ApprovalRequest, block until response or timeout in internal/daemon/daemon.go
- [X] T037 [US4] Add respond_approval RPC handler in internal/daemon/daemon.go registerRealHandlers()
- [X] T038 [P] [US4] Add respond_approval MCP tool registration and handler in internal/mcp/server.go and internal/mcp/tools.go
- [X] T039 [US4] Add list_pending_approvals RPC handler in internal/daemon/daemon.go
- [X] T040 [US4] Add `kingctl approvals` subcommand to list pending requests in cmd/kingctl/main.go
- [X] T041 [US4] Add `kingctl approve <id>` and `kingctl reject <id>` subcommands in cmd/kingctl/main.go
- [X] T042 [US4] Add approval request display in TUI Audit Hall — highlight pending requests, allow approve/reject with 'y'/'n' keys in internal/tui/components/audit.go
- [X] T043 [US4] Mark pending approvals as "expired" on daemon restart in internal/daemon/daemon.go Start()

**Checkpoint**: Sovereign Approval fully functional — blocks exec_in, approvable via TUI/CLI/MCP

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Performance, reliability, edge cases

- [X] T044 [P] Add periodic audit retention cleanup (every 6 hours via ticker) in internal/daemon/daemon.go
- [X] T045 [P] Add ingestion sampling logic (>1000 lines/sec → sample every 10th) with sampled=true flag in internal/audit/batcher.go
- [X] T046 [P] Add binary data handling — replace non-printable chars with placeholder in ingestion recording in internal/audit/batcher.go
- [ ] T047 Validate quickstart.md scenarios end-to-end (all 4 scenarios)
- [X] T048 Add AuditRecorder interface to internal/mcp/server.go to avoid import cycles (same pattern as PTYManager/ArtifactLedger)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 (T001-T003 complete)
- **US1 (Phase 3)**: Depends on Phase 2 — BLOCKS subsequent stories
- **US2 (Phase 4)**: Depends on Phase 2 + T010 (AuditRecorder wired)
- **US3 (Phase 5)**: Depends on US1 (Phase 3 — needs working get_audit_log)
- **US4 (Phase 6)**: Depends on Phase 2 + T022 (Action Trace in exec_in)
- **Polish (Phase 7)**: Depends on US1-US4 completion

### User Story Dependencies

- **US1 (P1)**: Foundation only — can start immediately after Phase 2
- **US2 (P2)**: Needs AuditRecorder (T010) — can start after T010, parallel with US1 remainder
- **US3 (P3)**: Needs get_audit_log working (T015-T016) — sequential after US1 core
- **US4 (P4)**: Needs exec_in trace (T022) — can start after T022, parallel with US3

### Within Each User Story

- Store methods → Recorder integration → Daemon wiring → MCP/RPC → TUI/CLI

### Parallel Opportunities

- T005 + T006 (store methods for ActionTrace and ApprovalRequest)
- T016 + T017 (MCP tool and TUI component)
- T025 + T026 (MCP tool and CLI)
- T038 + T040/T041 (MCP tool and CLI approve/reject)
- T044 + T045 + T046 (all polish tasks)

---

## Parallel Example: User Story 1

```bash
# After T010 (AuditRecorder wired), launch in parallel:
Task T016: "MCP get_audit_log tool in internal/mcp/server.go + tools.go"
Task T017: "TUI AuditModel in internal/tui/components/audit.go"

# After T015 (RPC handler), launch in parallel:
Task T018: "TUI tab 4 wiring in internal/tui/app.go"
Task T019: "kingctl audit subcommand in cmd/kingctl/main.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001-T003)
2. Complete Phase 2: Foundational (T004-T009)
3. Complete Phase 3: User Story 1 (T010-T020)
4. **STOP and VALIDATE**: `king dashboard` → tab 4 shows audit stream, `kingctl audit` works
5. This alone delivers core audit transparency

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 (Audit Stream) → Test → **MVP!**
3. Add US2 (Action Trace) → Test → exec_in traceability
4. Add US3 (Time-Travel) → Test → retrospective debugging
5. Add US4 (Sovereign Approval) → Test → human-in-the-loop control
6. Each story adds value without breaking previous stories

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story
- All new code in `internal/audit/` package (recorder, batcher, approval)
- Store extensions in existing `internal/store/db.go` + `migrations.go`
- Follow existing patterns: MCP tool registration, RPC handler, TUI component
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
