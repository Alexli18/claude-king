# Research: Claude King — AI Kingdom Orchestrator

**Date**: 2026-04-02 | **Status**: Complete

## 1. PTY Management in Go

**Decision**: `github.com/creack/pty` v2
**Rationale**: De-facto standard Go PTY library. Actively maintained successor to `kr/pty` (archived). Clean API: `pty.Start(cmd)` returns `*os.File` for read/write. Supports window resizing via `pty.Setsize()`.
**Alternatives considered**:
- `kr/pty` — archived, redirects to creack/pty
- `Netflix/go-expect` — wraps creack/pty with expect-style matching, too heavy for our use case
- Raw `syscall` — unnecessary complexity

## 2. MCP Server SDK for Go

**Decision**: `github.com/mark3labs/mcp-go`
**Rationale**: Community-standard Go SDK listed in official MCP SDK directory. Supports stdio and SSE transports, tool/resource/prompt registration. Actively maintained. Official Anthropic SDKs are TypeScript/Python only.
**Alternatives considered**:
- `metoro-io/mcp-golang` — less traction and community adoption
- Custom JSON-RPC implementation — feasible but unnecessary given mcp-go maturity

## 3. IPC via Unix Domain Sockets

**Decision**: Go stdlib `net.Listen("unix", path)` / `net.Dial("unix", path)`
**Rationale**: First-class UDS support in stdlib. No third-party dependency needed.
**Best practices**:
- `os.Remove` socket path before `Listen` to handle stale sockets
- Set `0700` permissions on socket directory for security
- Socket path: `.king/king.sock` within project directory
- Use `context`-aware connections via `net.ListenConfig`
**Alternatives considered**:
- gRPC over UDS — overkill for MCP's JSON-RPC protocol
- TCP localhost — less secure, port conflicts between Kingdoms

## 4. State Persistence (SQLite)

**Decision**: `modernc.org/sqlite` (pure Go)
**Rationale**: Zero CGo dependency enables `CGO_ENABLED=0` builds and simple cross-compilation. Performance is ~80-90% of CGo version — more than sufficient for session metadata and event logs. Use via `database/sql` interface.
**Alternatives considered**:
- `mattn/go-sqlite3` — faster for write-heavy workloads but requires CGo
- BoltDB/bbolt — simpler but lacks SQL query flexibility for event log queries
- Files/JSON — insufficient for concurrent access and querying

## 5. TUI Framework

**Decision**: `github.com/charmbracelet/bubbletea` v1 + `lipgloss` + `bubbles`
**Rationale**: Elm-architecture TUI framework. Stable v1 release. Rich component ecosystem.
**Key considerations**:
- Use `evertras/bubble-table` instead of `bubbles/table` for complex dashboards (filtering, sorting, pagination)
- Real-time updates via `p.Send()` from goroutines — never mutate model outside `Update` loop
- Alt-screen mode may conflict when nesting PTYs; test carefully
**Alternatives considered**:
- `rivo/tview` — more traditional widget toolkit, less composable
- `gdamore/tcell` — lower-level, more control but more boilerplate

## 6. Terminal Multiplexing Architecture

**Decision**: Custom implementation using creack/pty, referencing existing Go multiplexers
**Rationale**: No library exists for "PTY multiplexing as a package." Core pattern: one goroutine per PTY doing buffered reads, a central router dispatching input, event detection on output stream.
**Reference projects**:
- `aaronjanse/3mux` — Go terminal multiplexer, closest architectural reference for PTY management and input routing
- `mrusme/neonmodem` — Bubbletea-based TUI with multi-pane layout
**Architecture pattern**:
1. Each Vassal: goroutine reading PTY → ring buffer + event detection pipeline
2. Central Manager: routes commands to vassals, manages lifecycle
3. MCP Server: translates tool calls to manager operations
4. Event Bus: distributes detected events to subscribers (King session, TUI)

## 7. Configuration Format

**Decision**: YAML for `.king/kingdom.yml`, JSON for `vassal.json` (VMP protocol)
**Rationale**: YAML is standard for project configuration files (docker-compose, CI). JSON for `vassal.json` aligns with ecosystem conventions (package.json, tsconfig.json) and is easier for programmatic generation.
**Library**: `gopkg.in/yaml.v3` for YAML parsing
