# Claude King

**Observability and control plane for parallel Claude Code sessions.**

When you run Claude Code in multiple repos simultaneously, each instance is isolated. You switch terminals, search for errors, manually copy context between windows. King is a daemon that sits above your agents and gives you a single pane of glass.

[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/Protocol-MCP-blueviolet)](https://modelcontextprotocol.io)
[![CI](https://github.com/alexli18/claude-king/actions/workflows/ci.yml/badge.svg)](https://github.com/alexli18/claude-king/actions)
[![Release](https://img.shields.io/github/v/release/alexli18/claude-king)](https://github.com/alexli18/claude-king/releases)

![Demo](demo.gif)

---

## The problem

You run Claude Code in 3 repos at once. One agent breaks tests. Another writes an AWS key into a config file. A third hangs on a frozen PTY. You're the one watching for it — switching terminals, grepping logs, copying context between windows.

King watches your agents so you don't have to.

---

## What it does

- **One view across all repos.** King's TUI and MCP tools give Claude a live feed of every vassal: status, events, errors, artifacts.
- **Secret scanning on every artifact.** AWS keys, GitHub tokens, private keys, `.env` files — blocked before they reach the Ledger.
- **Auto-integrity contracts.** King fingerprints your repo (Go, Node, embedded) and installs matching health checks automatically.
- **Health guards with circuit breaker.** Port checks, log pattern matching, data rate monitoring, custom health scripts — open the circuit and block AI modifications when things go wrong.
- **Delegation control.** An AI session can take explicit control of a vassal (`delegate_control`) and release it. King enforces who controls what.
- **Structured audit trail.** Every command, artifact, and event is stored in SQLite. Replay what happened and when.

---

## Why not native Claude Code hooks + MCP?

Claude Code already has hooks and MCP support. Here's where they stop and where King starts:

| Capability | Claude Code hooks + MCP | King |
|---|---|---|
| Automate one session | ✅ | ✅ |
| Coordinate multiple sessions across repos | ❌ | ✅ |
| Shared event bus across all agents | ❌ | ✅ |
| Secret scanning on AI-generated artifacts | ❌ | ✅ |
| Persistent audit trail (SQLite) | ❌ | ✅ |
| Cross-repo artifact addressing (`king://artifacts/...`) | ❌ | ✅ |
| Agent health guards with circuit breaker | ❌ | ✅ |
| Delegation control + heartbeat warden | ❌ | ✅ |
| Works with serial/embedded devices (ESP32, NMEA) | ❌ | ✅ |

**Hooks** automate a single Claude session's lifecycle. **King** coordinates multiple sessions and repos under one daemon, with a shared event store, artifact ledger, and enforcement layer.

---

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/alexli18/claude-king/master/install.sh | bash
```

<details>
<summary>Build from source (requires Go 1.25+)</summary>

```bash
git clone https://github.com/alexli18/claude-king && cd claude-king
make build && make install-user
```

</details>

```bash
# Start your Kingdom
cd ~/your-project
king up --detach   # runs as background daemon, logs to .king/daemon.log

# Check status
king status
king list          # all kingdoms running on this machine
```

Add King as an MCP server so Claude can see your agents:

```json
{
  "mcpServers": {
    "king": {
      "command": "king",
      "args": ["mcp"]
    }
  }
}
```

Restart Claude Code. It now has live access to all your running agents via `list_vassals`, `get_events`, `exec_in`, `guard_status`, and more.

---

## Demo: 4 agents, one control plane

*3 repos, one AWS key leaked, one port down, one hanging PTY. King caught all three.*

```
$ king up --detach

# Claude Code in the api/ repo calls:
→ list_vassals()
← [api: running, firmware: running, tests: failed 12s ago, frontend: running]

# One agent leaked credentials:
← WARN FILE_BLOCKED path=config.yml reason=AWS_KEY_DETECTED

# Health guard fires because the API port is down:
← WARN GUARD_CIRCUIT_OPEN vassal=api guard_type=port_check consecutive_fails=3

# Claude tries to take control of api vassal — blocked:
→ delegate_control("api")
← Error: Guard 'port_check' (index 0) circuit open for vassal 'api'.
         Consecutive failures: 3. AI modifications blocked.

# After the port recovers, circuit closes automatically:
← WARN GUARD_CIRCUIT_CLOSED vassal=api guard_type=port_check

# Claude reads serial output from embedded device:
→ get_serial_events("firmware", "10m", "critical")
← [{"summary": "ESP32 panic: core 0 — likely stack overflow in WiFi task"}]
```

---

## Configuration

King is zero-config by default — `king up` works without any config file. Drop `.king/kingdom.yml` to declare vassals explicitly:

```yaml
name: my-project

vassals:
  - name: api
    command: go run ./cmd/server
    autostart: true
    env:
      PORT: "8080"
    guards:
      - type: port_check    # is the server actually up?
        port: 8080
        interval: 10
        threshold: 3
      - type: log_watch     # crash pattern detection
        fail_on: ["FATAL", "panic:"]
        interval: 5

  - name: firmware
    type: serial
    serial_port: /dev/ttyUSB0
    baud_rate: 115200
    serial_protocol: esp32
    autostart: true

  - name: tests
    command: go test ./... -v
    autostart: false

patterns:
  - name: build-fail
    regex: "FAIL|fatal error:"
    severity: error
    summary_template: "Build failure in {vassal}: {match}"
```

Run `king up` — King starts `api` and `firmware` automatically, monitors both, and Claude gets live events.

---

## Guard types

Declare health checks per vassal. King runs them on a ticker and opens the circuit after N consecutive failures — blocking AI modifications until the vassal recovers.

| Type | What it checks |
|---|---|
| `port_check` | TCP dial to `127.0.0.1:<port>` — expects open or closed |
| `log_watch` | Regex patterns in recent PTY output — fails on match |
| `data_rate` | Byte throughput from process output — fails below threshold |
| `health_check` | External script exit code — fails on non-zero or timeout |

```yaml
guards:
  - type: health_check
    exec: ./scripts/validate.sh
    timeout: 10
    threshold: 2
  - type: data_rate
    min: 100bps     # sensor must emit at least 100 bytes/sec
    interval: 10
```

When the circuit opens, `delegate_control` is rejected with a clear error. When the vassal recovers, the circuit closes automatically.

---

## Security and Hardening

Claude King is designed to reduce a specific set of risks that emerge when AI coding agents operate autonomously across multiple repositories:

- **Secret leakage via artifacts:** every file submitted to the Ledger is scanned for AWS credentials, GitHub tokens, private keys, and sensitive filenames before storage (`internal/security/scanner.go`).
- **Uncontrolled command execution:** `exec_in` supports a sovereign approval gate (`internal/daemon/daemon.go`) that blocks execution until a Sovereign explicitly authorizes the command.
- **Unhealthy agent escalation:** the guard circuit breaker (`internal/daemon/delegation_handlers.go`) blocks `delegate_control` when a vassal's health checks are failing.
- **Auditability:** all commands, artifacts, and events are stored in SQLite at `.king/king.db`, providing a replay-capable audit trail.

**What King does not currently guarantee:**
- Sandboxed or containerized command execution
- Network isolation between vassal processes
- Secret scanning of event payloads or task descriptions (exec_in output IS scanned by default)
- OS-level privilege separation between the daemon and vassals

For a full threat model and planned hardening roadmap, see [`security-research/`](security-research/) and [`docs/secure-bash-roadmap.md`](docs/secure-bash-roadmap.md).

---

## Security Quickstart

**What is enabled by default:**
- `exec_in` output is scanned for secrets (AWS keys, GitHub tokens, private keys). If a secret is detected, the command output is blocked and the agent receives `SENSITIVE_OUTPUT_BLOCKED`.
- A warning is logged at daemon startup if `sovereign_approval` is not configured.

**What you should enable before running agents you don't fully trust:**

```yaml
# kingdom.yml
settings:
  sovereign_approval: true        # require human approval for every exec_in command
  sovereign_approval_timeout: 300 # seconds before auto-reject (default: 300)
  scan_exec_output: true          # scan exec_in output for secrets (default: true, shown for clarity)
```

**What is not covered:** event payloads, task descriptions, and `dispatch_task` arguments are not scanned. No network isolation or OS-level sandboxing.

See [`security-research/`](security-research/) for the full threat model.

---

## Threat Model (Initial)

| Threat | Current mitigation | Gap |
|--------|-------------------|-----|
| **Command injection via exec_in** | Optional sovereign approval gate (`internal/daemon/daemon.go`) | Not enforced by default; no command denylist |
| **Secret leakage via artifacts** | Regex scanner on Ledger writes + exec_in output (`internal/security/scanner.go`) | Event payloads, task descriptions not scanned |
| **Unsafe artifact storage** | Secret scanner blocks before SQLite write | No encryption at rest; SQLite is world-readable |
| **Weak approval coverage** | exec_in gated when sovereign_approval configured | dispatch_task, register_artifact have no approval gate |
| **Local trust-boundary assumptions** | Daemon runs with user's OS privileges | No isolation between King daemon and vassal processes |

See [`security-research/`](security-research/) for detailed analysis of each threat.

---

## Security

Every artifact submitted to King's Ledger is scanned before storage:

| Threat | Detection |
|---|---|
| AWS credentials (`AKIA...`) | `AWS_KEY_DETECTED` |
| GitHub tokens (`ghp_...`) | `GITHUB_TOKEN_DETECTED` |
| Private keys (`-----BEGIN RSA PRIVATE KEY-----`) | `PRIVATE_KEY_DETECTED` |
| Sensitive files (`.env`, `id_rsa`, `*.pem`) | `FILENAME_BLACKLISTED` |

When a vassal registers, King fingerprints its repo and installs matching integrity contracts automatically:

| Project type | Auto-contracts |
|---|---|
| Go | `go-vet-error` |
| Node.js + test script | `npm-test-failure` |
| Node.js + ESLint | `eslint-error` |

No config required.

---

## MCP tools

| Tool | What Claude can do |
|---|---|
| `list_vassals()` | Status of every running agent |
| `exec_in(vassal, cmd)` | Run a command in a background terminal |
| `get_events(severity)` | Fetch errors and warnings across all repos |
| `get_serial_events(vassal, since, severity)` | Events from serial/embedded vassals |
| `read_artifact(name)` | Fetch a file from the shared Ledger |
| `read_neighbor(path)` | Read files from another vassal's repo |
| `guard_status(vassal?)` | Live circuit breaker state per guard |
| `delegate_control(vassal)` | Take exclusive control of a vassal |
| `delegate_release(vassal)` | Hand control back to King |
| `get_audit_log(limit, since)` | Full audit trail |

---

## Architecture

```
  YOUR CLAUDE CODE SESSIONS
  ┌──────────┐  ┌──────────┐  ┌──────────┐
  │ api/     │  │ firmware/│  │ tests/   │
  │ Claude   │  │ Claude   │  │ Claude   │
  └────┬─────┘  └────┬─────┘  └────┬─────┘
       │ MCP          │ MCP          │ MCP
       └──────────────┴──────────────┘
                      │
              ┌───────▼────────┐
              │  KING DAEMON   │
              │                │
              │  Event Store   │  ← SQLite
              │  Ledger        │  ← artifact addressing
              │  Secret Guard  │  ← scans before store
              │  Guard Runner  │  ← port/log/rate/script checks
              │  Audit Trail   │  ← full history
              └───────┬────────┘
                      │ Unix sockets
          ┌───────────┼───────────┐
          ▼           ▼           ▼
      vassal        vassal      vassal
      (api/)    (firmware/)   (tests/)
```

**Key facts:**
- Each project root gets its own daemon. `king list` shows all running kingdoms.
- Socket path: `.king/king-<sha256[:8]>.sock` — deterministic, collision-free.
- All state is in `.king/king.db` (SQLite). Guard state is in-memory only and resets on restart.
- Daemon sends SIGTERM to all vassals on `king down`.

---

## Platforms

| Platform | Status |
|---|---|
| macOS (arm64, amd64) | ✅ Supported |
| Linux (amd64, arm64) | ✅ Supported |
| Windows | ❌ Not supported (PTY dependency) |

---

## Roadmap

- [x] King daemon, PTY sessions, MCP server
- [x] Vassal protocol, Artifact Ledger
- [x] Semantic Sieve, event filtering, audit trail
- [x] Secret scanner, auto-integrity contracts
- [x] Serial vassal (ESP32 / NMEA / AT)
- [x] TUI dashboard (`king tui`)
- [x] Delegation control + heartbeat warden
- [x] Health guards with circuit breaker (`guard_status`)
- [x] Prebuilt binaries via GitHub Releases
- [ ] Event webhooks
- [ ] `king doctor` — full diagnostic output

---

*Built with Go · Runs on Unix · MIT License*
