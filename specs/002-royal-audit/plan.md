# Implementation Plan: The Royal Audit

**Branch**: `002-royal-audit` | **Date**: 2026-04-02 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-royal-audit/spec.md`

## Summary

The Royal Audit добавляет слой прозрачности в Claude King — три уровня аудита (Ingestion, Sieve, Action), TUI-таб "Audit Hall", Time-Travel фильтрацию и режим Sovereign Approval для подтверждения exec_in команд. Реализуется как расширение существующей архитектуры: новые SQLite-таблицы, новый пакет `internal/audit`, интеграция в daemon/sieve/pty/mcp/tui.

## Technical Context

**Language/Version**: Go 1.22+
**Primary Dependencies**: creack/pty v2, mark3labs/mcp-go, charmbracelet/bubbletea v1, modernc.org/sqlite, google/uuid
**Storage**: SQLite (существующая БД, расширение через migration v2)
**Testing**: go test (unit + integration)
**Target Platform**: macOS / Linux (CLI + daemon)
**Project Type**: CLI + daemon
**Performance Goals**: <2с фильтрация 100K записей, batch insert для ingestion
**Constraints**: Ingestion layer опционален (высокая нагрузка на I/O), retention policy обязателен
**Scale/Scope**: 1-10 вассалов, до 100K аудит-записей

## Constitution Check

*GATE: Constitution is a template without specific principles — PASS by default.*

No violations. No complexity tracking needed.

## Project Structure

### Documentation (this feature)

```text
specs/002-royal-audit/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0: research decisions
├── data-model.md        # Phase 1: entity definitions
├── quickstart.md        # Phase 1: integration scenarios
├── contracts/
│   └── audit-mcp-tools.md  # MCP tool + RPC contracts
└── tasks.md             # Phase 2 output (via /speckit.tasks)
```

### Source Code (repository root)

```text
internal/
├── audit/
│   ├── recorder.go       # AuditRecorder: central audit write logic
│   ├── batcher.go        # BatchWriter: buffered ingestion writes
│   └── approval.go       # ApprovalManager: sovereign approval channel logic
├── config/
│   └── types.go          # +AuditSettings fields in Settings struct
├── store/
│   ├── db.go             # +CreateAuditEntry, ListAuditEntries, CreateActionTrace, etc.
│   └── migrations.go     # +migration v2: audit_entries, action_traces, approval_requests tables
├── daemon/
│   └── daemon.go         # +AuditRecorder wiring, approval integration in exec_in handler
├── events/
│   └── sieve.go          # +audit hooks in ProcessLine (sieve layer logging)
├── pty/
│   └── session.go        # +TraceID in CommandResult, audit hooks in executeCommand
├── mcp/
│   ├── server.go         # +registerGetAuditLog, registerGetActionTrace, registerRespondApproval
│   └── tools.go          # +handleGetAuditLog, handleGetActionTrace, handleRespondApproval
├── tui/
│   ├── app.go            # +tabAudit, audit data fetching
│   └── components/
│       └── audit.go      # NEW: AuditModel, AuditView (scrollable + filterable)
└── cmd/
    └── kingctl/
        └── main.go       # +audit, approvals, approve, reject subcommands
```

**Structure Decision**: Новый пакет `internal/audit` содержит бизнес-логику аудита (recorder, batcher, approval). Store-уровень получает CRUD-методы. Интеграция в daemon/sieve/pty через callback-паттерн (как существующий onOutput).

## Integration Map

| Concern | File | Hook Point |
|---------|------|------------|
| Audit DB schema | store/migrations.go | migration v2 |
| Audit CRUD | store/db.go | New methods on Store |
| Ingestion logging | sieve.go ProcessLine | Before pattern matching loop |
| Sieve decision logging | sieve.go ProcessLine | After match/suppress decision |
| Action trace start | daemon.go exec_in handler | Before sess.ExecCommand |
| Action trace complete | daemon.go exec_in handler | After sess.ExecCommand returns |
| TraceID generation | pty/session.go executeCommand | Add to CommandResult |
| Sovereign approval gate | daemon.go exec_in handler | Between trace start and ExecCommand |
| TUI Audit Hall | tui/app.go + components/audit.go | Tab 4 |
| MCP tools | mcp/server.go + tools.go | 3 new tools |
| CLI commands | cmd/kingctl/main.go | 4 new subcommands |
| Config extension | config/types.go | Settings struct |
| Retention cleanup | daemon.go Start() | After existing DeleteOldEvents |

## Complexity Tracking

No constitution violations to justify.
