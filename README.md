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

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/Protocol-MCP-blueviolet)](https://modelcontextprotocol.io)

---

## The Manifesto

AI agents write your code. Fast. Relentlessly. Across 20 windows at once.

**But who watches the realm?**

Claude King is a **Sovereign Development** platform — a daemon that sits above your AI agents, watches every process, guards every artifact, and gives you one throne to rule them all. While Cursor generates, King verifies. While Claude commits, King audits. You are not a developer anymore. You are the Sovereign.

---

## Quick Start

```bash
# 1. Build
git clone https://github.com/alexli18/claude-king && cd claude-king
go build -o king ./cmd/king && go build -o kingctl ./cmd/kingctl

# 2. Rise
cd ~/your-project
king up             # foreground (logs to stdout)
king up --detach    # background daemon (logs to .king/daemon.log)

# 3. Rule
king list           # show all running kingdoms (cross-directory)
kingctl status

# 4. Stop
king down
```

That's it. Your Kingdom is running.

---

## The Hierarchy

Every Kingdom is governed by a chain of loyal subjects:

| Character | Binary / Package | Duty |
|---|---|---|
| 👑 **The King** | `king` daemon | Orchestrates the realm. Holds the Ledger. Issues the law. |
| ⚔️ **The Vassal** | `king-vassal` | Embedded agent in each repo. Sends tribute (artifacts) to the throne. |
| 🛡️ **The Royal Guard** | `internal/security` | Stands at the Ledger gates. No secret token enters the realm. |
| 🔮 **The Court Mage** | `internal/fingerprint` | Divines the nature of each project. Inscribes the appropriate integrity contracts. |
| ⚖️ **The Inquisitor** | `internal/daemon/auto_integrity` | Subjects every artifact to trial. Dirty code does not pass. |
| 📜 **The Chronicler** | `internal/store` (SQLite) | Records every action, every artifact, every failure. Forever. |
| 📯 **The Herald** | `internal/events` | Cries across the realm when builds fall and secrets are found. |

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

## Philosophy

```
You are not a developer.
You are a Sovereign.

Your code is your realm.
Your AI agents are your vassals.
King is your throne.

Let them build.
You rule.
```

---

## Roadmap

- [x] **Phase 1** — King daemon, PTY sessions, MCP server
- [x] **Phase 2** — Vassal protocol (VMP), Artifact Ledger
- [x] **Phase 3** — Semantic Sieve, event filtering, Royal Audit
- [x] **Phase 3.5** — Zero-config onboarding, Secret Scanner, Auto-Integrity
- [x] **Phase 3.6** — Serial vassal (ESP32/NMEA/AT), `get_serial_events` MCP tool, `king list` P2P registry
- [ ] **Phase 4** — TUI dashboard (`king tui`), event webhooks, Vector memory

---

*Built with Go · Runs on Unix · Governs AI*
