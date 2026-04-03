# Claude King

Run multiple Claude Code agents in parallel across your repos.
One MCP daemon to coordinate them all.

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/Protocol-MCP-blueviolet)](https://modelcontextprotocol.io)
[![CI](https://github.com/alexli18/claude-king/actions/workflows/ci.yml/badge.svg)](https://github.com/alexli18/claude-king/actions)

```
         ╔═══╗
        ╔╝ ♔ ╚╗         C L A U D E   K I N G
       ╔╝  ♦  ╚╗
      ╔╝ ◆   ◆ ╚╗      Sovereign AI Orchestration
     ╔╝◆   ●   ◆╚╗
    ╔╝ │  ╱│╲  │ ╚╗      ●── ● ──●
   ╔╝  ● ╱ │ ╲ ●  ╚╗    ╱│╲  │  ╱│╲
   ╚═══════════════╝   ● ● ● ● ● ● ●
```

> *In a world where Claude and Cursor write your code —*
> *King is the one who answers for it.*

![Demo](demo.gif)

---

## The Problem

You run Claude Code in 3 repos simultaneously. Each instance is blind to what the others are doing. You're the one watching for errors, switching terminals, copying context between windows.

King is a daemon that sits above your AI agents. It watches every process, scans every artifact for secrets, and surfaces errors across all repos in one place. Run 10 Claude instances in parallel — King keeps them coordinated.

*King assumes you'd rather govern than babysit.*

---

## Quick Start

```bash
# Build from source (Go 1.22+ required)
git clone https://github.com/alexli18/claude-king && cd claude-king
make build && make install-user   # installs to ~/.local/bin, patches PATH

# Start your Kingdom
cd ~/your-project
king up             # foreground (logs to stdout)
king up --detach    # background daemon (logs to .king/daemon.log)

# Inspect
king list           # all running kingdoms across your machine
king status         # current kingdom + vassals
king doctor         # health check

# Stop
king down
```

---

## How It Works

1. `king up` starts a daemon in your project root and creates a Unix socket at `.king/king-<hash>.sock`
2. Each `king-vassal` process (one per sub-repo) connects to that socket and registers itself
3. King monitors vassal output, matches it against patterns, and stores events in SQLite
4. You add King as an MCP server — Claude Code can then call `list_vassals()`, `get_events()`, `exec_in()` and get live data from all running agents
5. Every file reaching the Artifact Ledger is scanned for secrets before it's stored

**Topology:** each project directory gets its own King daemon. `king list` shows all running kingdoms across your machine. Each Claude Code window connects to the King in its project root via `king mcp`.

---

## Connect to Claude Code

Add King as an MCP server in your project's `.mcp.json` (or `~/.claude.json`):

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

Restart Claude Code. You'll have access to all MCP tools (`list_vassals`, `get_events`, `exec_in`, `read_artifact`, etc.) directly in your AI assistant.

---

## The Hierarchy

Every Kingdom is governed by a chain of loyal subjects:

| Character | Binary / Package | Duty |
|---|---|---|
| 👑 **The King** | `king` daemon | Orchestrates the realm. Holds the Ledger. Issues the law. |
| ⚔️ **The Vassal** | `king-vassal` | Embedded agent in each repo. Sends tribute (artifacts) to the throne. |
| 🛡️ **The Royal Guard** | `internal/security` | Blocks AWS keys, private keys, `.env` files before they reach the Ledger. |
| 🔮 **The Court Mage** | `internal/fingerprint` | Auto-detects project type (Go, Node) and applies matching integrity rules. |
| ⚖️ **The Inquisitor** | `internal/daemon/auto_integrity` | Runs integrity contracts against vassal output. Flags violations automatically. |
| 📜 **The Chronicler** | `internal/store` (SQLite) | Persists all events, artifacts, and audit records. |
| 📯 **The Herald** | `internal/events` | Emits structured events when builds fail, secrets are detected, or patterns match. |

```
  YOUR TERMINAL (Claude Code / Cursor)
         │  MCP Tools
         ▼
  ┌─────────────────────────────────────┐
  │         👑  THE KING  (daemon)      │
  │                                     │
  │  📜 Ledger ──► 🛡️ Royal Guard       │
  │  🔮 Court Mage                      │
  │  ⚖️ Inquisitor                      │
  │  📯 Herald                          │
  └──────┬──────────┬──────────┬────────┘
         │          │          │  Unix Sockets
         ▼          ▼          ▼
      ⚔️ Vassal  ⚔️ Vassal  ⚔️ Vassal
      (api/)    (firmware/) (ml-model/)
```

---

## Terminal Preview

```
$ king up

time=... level=INFO msg="daemon started" root=/my-project pid=48291
time=... level=INFO msg="vassal started" name=api
time=... level=INFO msg="vassal started" name=esp32-watch
time=... level=INFO msg="vassal started" name=tests
time=... level=INFO msg="event detected" pattern=generic-error severity=error vassal=tests
time=... level=WARN msg=FILE_BLOCKED vassal=api path=config.yml reason=AWS_KEY_DETECTED

$ king list

KINGDOM                              STATUS       PID      SOCKET
------------------------------------------------------------------------
/Users/alex/my-project               running      48291    .king/king-a3f9c1.sock
```

```
$ kingctl status

KINGDOM     my-project          ● running  pid 48291
SOCKET      .king/king-a3f9c1.sock
ROOT        /Users/alex/my-project

VASSAL          STATUS     LAST SEEN    EVENTS
api             🟢 idle    2s ago       0 errors
esp32-watch     🟢 active  now          3 warnings
tests           🔴 failed  12s ago      2 errors

INTEGRITY       go-vet-error (auto) · eslint-error (auto)
LEDGER          4 artifacts  ·  0 blocked  ·  1 flagged
```

---

## Configuration

King is zero-config by default. Drop a `kingdom.yml` in `.king/` to declare your vassals:

### Simple example (Go project)

```yaml
# .king/kingdom.yml
name: my-project

vassals:
  - name: server
    command: go run ./cmd/server
    autostart: true
    env:
      PORT: "8080"

  - name: tests
    command: go test ./... -v
    autostart: false
```

Run `king up` — King starts `server` automatically. Trigger tests on demand via Claude: `exec_in("tests", "go test ./...")`.

### Full example (with serial/ESP32)

```yaml
# .king/kingdom.yml
name: my-project

vassals:
  - name: api
    command: go run ./cmd/server
    autostart: true
    env:
      PORT: "8080"

  - name: esp32-watch
    type: serial              # native serial reader (no minicom needed)
    serial_port: /dev/ttyUSB0
    baud_rate: 115200
    serial_protocol: esp32   # "esp32" | "nmea" | "at" | "" (auto)
    autostart: true

  - name: tests
    command: go test ./... -watch
    autostart: false

patterns:
  - name: esp32-panic
    regex: 'panic: core \d+'
    severity: critical
    source: esp32-watch
    summary_template: "ESP32 panic: {match}"
  - name: build-fail
    regex: "FAIL|fatal error:"
    severity: error
    summary_template: "Build failure in {vassal}: {match}"
```

### Vassal Zero-Config

A vassal repo? No flags needed:

```bash
cd ~/firmware
king-vassal   # auto-discovers socket, auto-detects name from cwd
```

The **Court Mage** fingerprints the directory and inscribes integrity contracts automatically.

---

## Security & Integrity

### Secret Scanner

Every artifact submitted to the Ledger passes inspection. Blocked threats:

| Threat | Log code |
|---|---|
| Sensitive filenames (`.env`, `id_rsa`, `*.pem`) | `FILENAME_BLACKLISTED` |
| Sensitive extensions (`.pem`, `.key`, `.p12`) | `EXTENSION_BLOCKED` |
| AWS credentials (`AKIA...`) | `AWS_KEY_DETECTED` |
| GitHub tokens (`ghp_...`, `ghs_...`) | `GITHUB_TOKEN_DETECTED` |
| Private keys (`-----BEGIN RSA PRIVATE KEY-----`) | `PRIVATE_KEY_DETECTED` |

```
level=WARN msg=FILE_BLOCKED path=config.yml reason=AWS_KEY_DETECTED
```

### ⚖️ Inquisitor — Auto-Integrity

When a Vassal registers, the **Court Mage** fingerprints its repo. The **Inquisitor** automatically inscribes matching contracts:

| Project Type | Auto-Contracts |
|---|---|
| Go | `go-vet-error` — catches vet errors in output |
| Node + test script | `npm-test-failure` — catches FAIL/failing lines |
| Node + eslint | `eslint-error` — catches linter violations |

No configuration required. The realm governs itself.

---

## Architecture

### The Ledger (Artifact Protocol)

Vassals produce files. The Ledger tracks them by name, version, and checksum. Consumers reference them as `king://artifacts/<name>`:

```
⚔️ firmware-vassal  →  ledger.Register("firmware.bin", ...)
                            ↓
👑 King Daemon       →  ledger.Resolve("firmware.bin")
                            ↓
⚔️ flash-vassal     →  flash(artifact.FilePath)
```

### Socket Discovery

King creates a deterministic socket per project root:

```
.king/king-<sha256[:8]>.sock
```

Vassals find it by walking up the directory tree — like `git` finds `.git`.

---

## MCP Tools (Scepter)

Connect Claude Code to your Kingdom by adding the MCP server. Then your AI has:

| Tool | What it does |
|---|---|
| `list_vassals()` | Status of every vassal |
| `exec_in(vassal, cmd)` | Run a command in a background terminal |
| `read_artifact(name)` | Fetch a file from the Ledger by name |
| `read_neighbor(path)` | Read files from another vassal's repo |
| `get_events(severity)` | Fetch recent errors and warnings |
| `get_serial_events(vassal, since, severity)` | Fetch events from a serial vassal (ESP32, NMEA, AT) |

```
Claude: "The firmware is crashing. Let me investigate."
→ get_serial_events("esp32-watch", "1h", "critical")
→ [{"severity":"critical","pattern":"esp32-panic","summary":"ESP32 panic: panic: core 0"}]
→ "Core 0 panicked — likely a stack overflow in the WiFi task."
```

---

---

## Roadmap

- [x] **Phase 1** — King daemon, PTY sessions, MCP server
- [x] **Phase 2** — Vassal protocol (VMP), Artifact Ledger
- [x] **Phase 3** — Semantic Sieve, event filtering, Royal Audit
- [x] **Phase 3.5** — Zero-config onboarding, Secret Scanner, Auto-Integrity
- [x] **Phase 3.6** — Serial vassal (ESP32/NMEA/AT), `get_serial_events` MCP tool, `king list` P2P registry
- [x] **Phase 4** — TUI dashboard (`king tui`)
- [ ] **Phase 5** — Event webhooks, Vector memory

---

*Built with Go · Runs on Unix · Governs AI*
