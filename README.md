# Claude King

**Observability and control plane for parallel Claude Code sessions.**

When you run Claude Code in multiple repos simultaneously, each instance is isolated. You switch terminals, search for errors, manually copy context between windows. King is a daemon that sits above your agents and gives you a single pane of glass.

[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/Protocol-MCP-blueviolet)](https://modelcontextprotocol.io)
[![CI](https://github.com/alexli18/claude-king/actions/workflows/ci.yml/badge.svg)](https://github.com/alexli18/claude-king/actions)
[![Release](https://img.shields.io/github/v/release/alexli18/claude-king)](https://github.com/alexli18/claude-king/releases)

![Claude King TUI — live dashboard for all your AI agents](demo.gif)

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
cd ~/your-project
king up --detach   # background daemon, logs to .king/daemon.log
king status        # check kingdom health
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

Restart Claude Code. It now has live access to all your running agents.

---

## Before / After

**Without King:**
You run 3 Claude Code sessions. One breaks tests — you notice 10 minutes later. Another writes an AWS key to a config file — you catch it only on commit. A third hangs — you have no idea.

**With King:**
1. `king up --detach` — once.
2. `list_vassals` → live status of every session. `get_events` → all errors, across all repos, in one call.
3. An agent becomes unhealthy? Circuit breaker blocks it automatically.

---

## What it does

- **One view across all repos.** TUI and MCP tools give Claude a live feed of every vassal: status, events, errors, artifacts.
- **Multi-AI routing.** Vassals can run Claude Code, OpenAI Codex, or Google Gemini. `list_vassals` exposes `type` and `specialization` so the sovereign can route tasks to the right agent.
- **Secret scanning.** AWS keys, GitHub tokens, private keys, `.env` files — blocked before they reach the Ledger.
- **Auto-integrity contracts.** King fingerprints your repo (Go, Node, embedded) and installs matching health checks automatically.
- **Health guards with circuit breaker.** Port checks, log pattern matching, data rate monitoring, custom health scripts — open the circuit and block AI modifications when things go wrong.
- **Delegation control.** An AI session can take exclusive control of a vassal (`delegate_control`) and release it.
- **Structured audit trail.** Every command, artifact, and event is stored in SQLite. Replay what happened and when.
- **Event webhooks.** HTTP POST with HMAC-SHA256 signing, retry, and event filtering — bridge to Slack, CI, or any HTTP endpoint.

---

## Why not native Claude Code hooks + MCP?

| Capability | Claude Code hooks + MCP | King |
|---|---|---|
| Automate one session | Yes | Yes |
| Coordinate multiple sessions across repos | — | Yes |
| Shared event bus across all agents | — | Yes |
| Secret scanning on AI-generated artifacts | — | Yes |
| Persistent audit trail (SQLite) | — | Yes |
| Cross-repo artifact addressing (`king://artifacts/...`) | — | Yes |
| Agent health guards with circuit breaker | — | Yes |
| Delegation control + heartbeat warden | — | Yes |
| Serial/embedded devices (ESP32, NMEA) | — | Yes |
| Multi-AI vassals (Claude, Codex, Gemini) | — | Yes |

**Hooks** automate a single Claude session's lifecycle. **King** coordinates multiple sessions and repos under one daemon, with a shared event store, artifact ledger, and enforcement layer.

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
← Error: Guard 'port_check' circuit open for vassal 'api'.
         Consecutive failures: 3. AI modifications blocked.

# After the port recovers, circuit closes automatically:
← WARN GUARD_CIRCUIT_CLOSED vassal=api guard_type=port_check
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
      - type: port_check
        port: 8080
        interval: 10
        threshold: 3
      - type: log_watch
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

  # AI vassals — King launches king-vassal subprocess for each
  - name: coder
    type: claude
    repo_path: ./services/api
    model: claude-opus-4-6
    specialization: "Go, REST APIs"

  - name: frontend
    type: codex
    repo_path: ./services/web
    model: o4-mini
    specialization: "TypeScript, React"

  - name: analyst
    type: gemini
    repo_path: ./services/data
    model: gemini-2.0-flash
    specialization: "data analysis, SQL"

patterns:
  - name: build-fail
    regex: "FAIL|fatal error:"
    severity: error
    summary_template: "Build failure in {vassal}: {match}"

settings:
  webhooks:
    - url: https://hooks.slack.com/services/...
      on: [error, critical, guard_circuit_open]
      secret: mysecret  # enables HMAC-SHA256 X-King-Signature header
```

See [`docs/getting-started.md`](docs/getting-started.md) for the full configuration reference.

---

## Guard types

Health checks per vassal. King runs them on a ticker and opens the circuit after N consecutive failures — blocking AI modifications until the vassal recovers.

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
    min: 100bps
    interval: 10
```

---

## MCP tools

| Tool | What Claude can do |
|---|---|
| `list_vassals()` | Status, type (`claude`/`codex`/`gemini`/`shell`), and `specialization` of every agent |
| `exec_in(vassal, cmd)` | Run a command in a background terminal |
| `get_events(severity)` | Fetch errors and warnings across all repos |
| `get_serial_events(vassal, since, severity)` | Events from serial/embedded vassals |
| `read_artifact(name)` | Fetch a file from the shared Ledger |
| `read_neighbor(path)` | Read files from another vassal's repo |
| `guard_status(vassal?)` | Live circuit breaker state per guard |
| `delegate_control(vassal)` | Take exclusive control of a vassal |
| `delegate_release(vassal)` | Hand control back to King |
| `get_audit_log(limit, since)` | Full audit trail |
| `dispatch_task(vassal, task)` | Dispatch a task to an AI vassal |

---

## Security

Claude King reduces risks that emerge when AI agents operate autonomously across multiple repositories.

**Enabled by default:**
- Artifact scanning — AWS keys, GitHub tokens, private keys, `.env` files are blocked before storage
- `exec_in` output scanning — secrets in command output are replaced with `SENSITIVE_OUTPUT_BLOCKED`
- Auto-integrity contracts — King fingerprints your repo (Go, Node) and installs matching health checks
- Guard circuit breaker — blocks `delegate_control` when health checks fail

**Enable for untrusted agents:**

```yaml
settings:
  sovereign_approval: true        # require human approval for every exec_in
  sovereign_approval_timeout: 300 # seconds before auto-reject
```

**Not currently covered:** event payloads and task descriptions are not scanned. No network isolation or OS-level sandboxing between daemon and vassals.

<details>
<summary>Threat model</summary>

| Threat | Current mitigation | Gap |
|--------|-------------------|-----|
| Command injection via exec_in | Optional sovereign approval gate | Not enforced by default; no command denylist |
| Secret leakage via artifacts | Regex scanner on Ledger writes + exec_in output | Event payloads, task descriptions not scanned |
| Unsafe artifact storage | Secret scanner blocks before SQLite write | No encryption at rest; SQLite is world-readable |
| Weak approval coverage | exec_in gated when configured | dispatch_task, register_artifact have no approval gate |
| Local trust-boundary | Daemon runs with user's OS privileges | No isolation between daemon and vassal processes |

See [`security-research/`](security-research/) for detailed analysis and [`docs/secure-bash-roadmap.md`](docs/secure-bash-roadmap.md) for the hardening roadmap.

</details>

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

- Each project root gets its own daemon. `king list` shows all running kingdoms.
- Socket path: `.king/king-<sha256[:8]>.sock` — deterministic, collision-free.
- All state is in `.king/king.db` (SQLite). Guard state is in-memory only.
- Daemon sends SIGTERM to all vassals on `king down`.

---

## Platforms

| Platform | Status |
|---|---|
| macOS (arm64, amd64) | Supported |
| Linux (amd64, arm64) | Supported |
| Windows | Not supported (PTY dependency) |

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
- [x] Multi-AI vassals — Claude Code, OpenAI Codex, Google Gemini
- [x] Event webhooks (HTTP POST with HMAC signing, retry, filtering)
- [ ] `king doctor` — full diagnostic output
- [ ] Slack integration — bridge from solo dev to team workflows

---

*Built with Go · Runs on Unix · MIT License*
