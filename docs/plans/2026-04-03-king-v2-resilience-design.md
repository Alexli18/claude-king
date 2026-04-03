# King v2 Resilience Design

**Date:** 2026-04-03
**Status:** Approved

## Goal

Three improvements to make King more resilient and self-aware:
1. **Process Supervision** — vassals restart automatically; no zombie processes
2. **Hardware Autodetect** — `serial_port: "auto:esp32"` instead of hardcoded `/dev/ttyUSB0`
3. **Kingdom ID** — ID propagated to `king list`, MCP responses, `vassal.register`, `king prompt-info`

---

## Feature 1: Process Supervision (PGID + Restart)

### Approach

Use `syscall.SysProcAttr{Setpgid: true}` when starting vassal processes. This puts each vassal and its children in a separate process group. On shutdown, King kills the entire group with `syscall.Kill(-pgid, SIGTERM)`, then SIGKILL after 3s.

A restart goroutine waits on `cmd.Wait()`. When the process exits (unexpectedly), it restarts with exponential backoff: 1s → 2s → 4s → 8s → ... → 60s max. Restart stops when daemon context is cancelled.

### Config

`restart_policy` field already exists in `VassalConfig`:
- `"always"` (default) — restart on any exit
- `"no"` — no restart

### Struct changes

```go
type vassalProc struct {
    process     *os.Process
    pgid        int
    restartCount int
    lastRestart  time.Time
}
```

Replace `vassalProcs map[string]*os.Process` with `vassalProcs map[string]*vassalProc`.

### Shutdown sequence

```
for each vassal proc:
    syscall.Kill(-pgid, SIGTERM)
    after 3s: syscall.Kill(-pgid, SIGKILL)
```

### Files

| File | Change |
|------|--------|
| `internal/daemon/daemon.go` | Replace `*os.Process` with `*vassalProc`; add restart goroutine; PGID kill on shutdown |

---

## Feature 2: Hardware Autodetect

### Syntax

In `kingdom.yml`:
```yaml
vassals:
  - name: firmware
    type: serial
    serial_port: "auto:esp32"   # or auto:ftdi, auto:gps, auto:any
    baud_rate: 115200
```

### VID/PID Table

```go
var knownDevices = map[string][]string{
    "esp32": {"10C4:EA60", "1A86:7523"},          // CP2102, CH340
    "ftdi":  {"0403:6001", "0403:6015"},           // FT232R, FT231X
    "gps":   {"067B:2303", "1546:01A7"},           // PL2303, u-blox
}
```

`"auto:any"` matches any `/dev/ttyUSB*` or `/dev/ttyACM*` (Linux) / `/dev/tty.usbserial-*` or `/dev/tty.SLAB_USBtoUART*` (macOS).

### Discovery logic

**Linux:** Read `/sys/class/tty/*/device/idVendor` + `idProduct`. Build `VVVV:PPPP`, look up in table, return `/dev/tty<name>`.

**macOS:** Parse `ioreg -p IOUSB -l -w0` for `idVendor` + `idProduct`. Fallback: glob `/dev/tty.SLAB_USBtoUART*` and `/dev/tty.usbserial-*` for `"auto:any"`.

Returns first match. If multiple matches, returns first (sorted lexicographically for determinism).

### Validation

`config.Validate()`: if `serial_port` starts with `"auto:"`, accept as valid (don't require static path). The actual discovery happens at daemon start, not at config load time.

### Error handling

If `"auto:esp32"` finds nothing → daemon logs error and skips that vassal (does not crash). MCP `vassal.status` reports `"no device found"`.

### Files

| File | Change |
|------|--------|
| `internal/discovery/serial.go` | New file: `FindSerialPort(hint string) (string, error)` |
| `internal/config/config.go` | Allow `"auto:*"` in `Validate()` |
| `internal/daemon/daemon.go` | Call `FindSerialPort` when starting `type:serial` vassal |

---

## Feature 3: Kingdom ID

### `king list` output

```
KINGDOM                           STATUS    PID       ID
/Users/alex/Desktop/FIX           running   12345     87678289
  vassals:
    firmware  (/emwirs-esp32-firmware)  alive
```

Short ID = first 8 hex chars of UUID.

### `king status` command

New subcommand `king status` (or updates existing one):

```
Kingdom: FIX
ID:      87678289-4b2a-...
Root:    /Users/alex/Desktop/FIX
Status:  running
PID:     12345
```

Implementation: dial daemon socket, call new RPC `kingdom.status`.

### MCP responses

All tool responses include `kingdom_id` field (short 8-char form) in the result metadata. Example:

```json
{
  "kingdom_id": "87678289",
  "events": [...]
}
```

This is added in `internal/mcp/server.go` or `tools.go` — wrap all tool results.

### `vassal.register` — kingdom_id verification

```json
{"name": "firmware", "repo_path": "...", "socket": "...", "pid": 123, "kingdom_id": "87678289"}
```

`kingdom_id` is optional. If provided and doesn't match daemon's `kingdom.ID`, return error: `"kingdom_id mismatch"`. If absent, accept (backward compat).

### `king prompt-info`

New subcommand. Dials local kingdom socket (from cwd, walks up), returns one line:

```
👑 FIX:87678289
```

If no kingdom found or daemon not running: exits silently (no output, exit code 0 — safe for prompt).

User setup in `~/.zshrc`:
```bash
RPROMPT='$(king prompt-info 2>/dev/null)'
```

### `kingdom.status` RPC

New daemon RPC method:
```json
// request: {}
// response:
{
  "id": "87678289-4b2a-...",
  "name": "FIX",
  "root": "/Users/alex/Desktop/FIX",
  "pid": 12345,
  "status": "running"
}
```

### Files

| File | Change |
|------|--------|
| `internal/daemon/daemon.go` | Add `kingdom.status` RPC handler |
| `internal/mcp/tools.go` | Add `kingdom_id` to all tool results |
| `cmd/king/main.go` | Update `king list`; add `king status`; add `king prompt-info` |

---

## Non-Goals

- cgroups v2 (overkill)
- Socket healthcheck polling (process-exit restart is enough)
- Automatic ZSH hook installation (user sets up PS1 manually)
- Persisting restart count across daemon restarts
