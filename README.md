```
         ╔══╗
        ╔╝♔ ╚╗         C L A U D E   K I N G
       ╔╝  ♦  ╚╗
      ╔╝ ◆   ◆ ╚╗      Sovereign AI Orchestration
     ╔╝◆   ●   ◆╚╗
    ╔╝ │  ╱│╲  │ ╚╗     ● ── ● ── ●
   ╔╝  ● ╱ │ ╲ ● ╚╗    ╱│╲  │  ╱│╲
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
king up

# 3. Rule
kingctl status
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
  │  📜 Ledger ──► 🛡️ Royal Guard      │
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

👑 Kingdom: my-project  [running]
├── ⚔️  api          🟢 running    idle 3s
├── ⚔️  esp32-watch  🟢 running    streaming
└── ⚔️  tests        🔴 failed     ⚠ 2 errors detected

🔮 Court Mage: Go project detected — contracts inscribed: [go-vet-error]
⚖️ Inquisitor: Artifact 'build.bin' passed integrity checks
🛡️ Royal Guard: Halt! Secret token spotted in config.yml — artifact banished
📯 Herald: [api] ERROR: connection refused on :8080
```

```
$ kingctl status

KINGDOM     my-project          ● running  pid 48291
SOCKET      .king/king-a3f9c1.sock
REALM       /Users/alex/my-project

VASSAL          STATUS     LAST SEEN    EVENTS
api             🟢 idle    2s ago       0 errors
esp32-watch     🟢 active  now          3 warnings
tests           🔴 failed  12s ago      2 errors

INTEGRITY       go-vet-error (auto) · eslint-error (auto)
LEDGER          4 artifacts  ·  0 blocked  ·  1 banished
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
    command: minicom -D /dev/ttyUSB0
    autostart: true
    repo_path: ../firmware   # ← Vassal declares its own kingdom

  - name: tests
    command: go test ./... -watch
    autostart: false

patterns:
  - name: panic
    regex: "panic:|fatal error:"
    severity: critical
    summary_template: "💀 Vassal {vassal} is down: {match}"
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

### 🛡️ Royal Guard — Secret Scanner

Every artifact submitted to the Ledger passes inspection. The Guard blocks:

| Threat | Example |
|---|---|
| Sensitive filenames | `.env`, `id_rsa`, `*.pem`, `credentials.*` |
| AWS credentials | `AWS_ACCESS_KEY_ID=AKIA...` |
| GitHub tokens | `ghp_...`, `ghs_...` |
| Private keys | `-----BEGIN RSA PRIVATE KEY-----` |
| OpenAI keys | `sk-...` (48 chars) |

```
🛡️ Royal Guard: Halt! INVALID_SECURITY: filename:blacklisted:.env
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
| `list_vassals()` | Status of every vassal in the realm |
| `exec_in(vassal, cmd)` | Run a command in a background terminal |
| `read_artifact(name)` | Fetch a file from the Ledger by name |
| `read_neighbor(path)` | Read files from another vassal's repo |
| `get_events(severity)` | Fetch recent errors and warnings |

```
Claude: "The tests are failing in the firmware vassal. Let me check."
→ exec_in("esp32-watch", "minicom --send test-payload.bin")
→ get_events("error")
→ "The ESP32 is rejecting your JSON timestamp format. Fix it in api/types.go."
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
- [x] **Phase 3** — Semantic Sieve (RTK), event filtering, Royal Audit
- [x] **Phase 3.5** — Zero-config onboarding, Secret Scanner, Auto-Integrity
- [ ] **Phase 4** — TUI dashboard (`king tui`), Herald webhooks, Vector memory

---

*Built with Go · Runs on Unix · Governs AI*
