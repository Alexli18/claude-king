# Implementation Plan: Claude King — AI Kingdom Orchestrator

**Branch**: `001-ai-kingdom-orchestrator` | **Date**: 2026-04-02 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-ai-kingdom-orchestrator/spec.md`

## Summary

Claude King is an autonomous host orchestrator written in Go that manages PTY sessions (Vassals) from a central daemon (King), exposing MCP tools for AI agent integration. The system enables zero-switching terminal workflows by coordinating multiple terminal sessions, filtering log noise via semantic sieve, and sharing artifacts between agents — all from a single Claude Code session.

## Technical Context

**Language/Version**: Go 1.22+
**Primary Dependencies**: creack/pty v2 (PTY), mark3labs/mcp-go (MCP server), charmbracelet/bubbletea v1 (TUI), modernc.org/sqlite (state storage)
**Storage**: SQLite (session state, event logs, artifact registry) + filesystem (artifacts, config)
**Testing**: Go stdlib `testing` + testify for assertions
**Target Platform**: macOS (primary), Linux (secondary) — Unix-like with PTY support
**Project Type**: CLI tool + daemon + MCP server
**Performance Goals**: Kingdom startup <10s, exec_in response <2s, event detection <5s
**Constraints**: Single-machine only, <100MB memory for daemon, no CGo dependencies
**Scale/Scope**: 10-50 simultaneous Kingdoms, 2-20 vassals per Kingdom

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution is unpopulated (template only). No gates to enforce. Proceeding.

## Project Structure

### Documentation (this feature)

```text
specs/001-ai-kingdom-orchestrator/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (MCP tool schemas, VMP protocol)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
cmd/
├── king/                # King daemon entry point
│   └── main.go
└── kingctl/             # CLI utility entry point
    └── main.go

internal/
├── daemon/              # Daemon lifecycle, signal handling
│   ├── daemon.go
│   └── kingdom.go
├── pty/                 # PTY session management (Vassal)
│   ├── session.go
│   └── manager.go
├── mcp/                 # MCP server & Scepter Tools
│   ├── server.go
│   └── tools.go
├── events/              # Semantic Sieve, event detection
│   ├── sieve.go
│   └── patterns.go
├── artifacts/           # Artifact Ledger
│   ├── ledger.go
│   └── resolver.go
├── store/               # SQLite state persistence
│   ├── db.go
│   └── migrations.go
├── config/              # .king directory config loading
│   └── config.go
└── tui/                 # Bubbletea dashboard (Phase 4)
    ├── app.go
    └── components/

tests/
├── integration/         # Multi-component tests (daemon + PTY + MCP)
└── unit/                # Package-level unit tests (colocated preferred)
```

**Structure Decision**: Single project layout with `cmd/` for entry points and `internal/` for all packages. No monorepo, no frontend — this is a pure Go CLI/daemon. Tests are colocated with packages for unit tests, with `tests/integration/` for cross-package integration tests.

## Complexity Tracking

No constitution violations to justify.

## Implementation Phases

### Phase 1 — MVP: King Daemon + PTY Sessions (User Stories 1, 2)

Core daemon that creates/manages PTY sessions and exposes basic MCP tools.

**Deliverables:**
- `king up` / `king down` commands
- PTY session creation and management
- Unix Domain Socket for IPC
- MCP server with `list_vassals()`, `exec_in(target, cmd)`
- `.king/` directory structure and config loading
- `kingctl` CLI for non-LLM interaction
- SQLite state persistence

### Phase 2 — Command Throne: Full MCP Integration (User Story 2 extended, 4)

Enhanced command execution and artifact sharing.

**Deliverables:**
- Streaming output from `exec_in`
- `read_neighbor(path)` MCP tool
- Artifact Ledger with `king://artifacts/` scheme
- VMP protocol (`vassal.json`) parsing
- Command queueing for concurrent `exec_in` to same vassal

### Phase 3 — Global Awareness: Event Detection (User Story 3)

Semantic Sieve and cross-terminal alerting.

**Deliverables:**
- Output monitoring with configurable patterns
- Semantic Sieve for noise reduction (90% target)
- Context injection into King session prompts
- Event correlation with recent changes
- Alert routing and notification system

### Phase 4 — Kingdom Management + TUI (User Stories 5, 6)

Multi-Kingdom isolation and visual dashboard.

**Deliverables:**
- Full Kingdom isolation (sockets, ports, state)
- Stale state detection and cleanup
- Bubbletea TUI dashboard
- Real-time vassal status display
- Interactive vassal selection and command execution from TUI
