# Hardware Focus v1 — Design Document

**Date:** 2026-04-03
**Status:** Approved
**Scope:** Serial/UART monitoring, NMEA/AT protocol patterns, machine-readable logs, plugin security scanner, P2P registry

---

## Context & Motivation

King's real differentiator is bridging LLMs and hardware. ESP32, GPRS modules, GPS trackers all communicate over serial. Today a developer watches raw UART output in a terminal while Claude sees nothing. This design makes King the translator between `/dev/ttyUSB0` and Claude.

Four initiatives in one plan:
1. **H — Hardware Serial**: `type: serial` vassal, ESP32/NMEA/AT auto-patterns, `get_serial_events` MCP tool
2. **L — Machine-readable Logs**: Remove lore strings, use structured key=value log lines
3. **P — Plugin Security Scanner**: Delegate scanning to `gitleaks`/`trufflehog` via exit code
4. **R — P2P Registry**: `~/.king/registry.json` + `king list` without a new daemon

---

## H: Hardware Serial

### Architecture

`type: serial` is a VassalConfig shorthand. The daemon expands it into a shell command run through the existing `pty.Manager`. No new dependencies.

**Expansion logic** (`daemon.go`, `startVassals`):
```
stty -F <serial_port> <baud_rate> raw -echo && cat <serial_port>
```

If `stty` is unavailable (macOS: `/dev/cu.*`), falls back to:
```
cat <serial_port>
```
with baud rate set via `ioctl` before opening (handled by a helper function).

### VassalConfig Extensions

```go
type VassalConfig struct {
    // existing fields ...

    // Serial-specific (only used when Type == "serial")
    SerialPort     string `yaml:"serial_port,omitempty"`
    BaudRate       int    `yaml:"baud_rate,omitempty"`       // default: 115200
    SerialProtocol string `yaml:"serial_protocol,omitempty"` // "esp32" | "nmea" | "at" | "" (auto)
}
```

### Protocol Detection

If `serial_protocol` is empty, auto-detect by `baud_rate`:
- 4800, 9600, 19200, 38400 → `nmea` (standard GPS baud rates)
- 115200 → `esp32` (ESP-IDF default)
- Other → no auto-patterns, user adds custom `patterns:` block

`serial_protocol: at` is always explicit (AT-command baud rate overlaps with others).

### Auto-Patterns (DefaultContracts)

Three new `ProjectType` constants: `ProjectTypeESP32`, `ProjectTypeNMEA`, `ProjectTypeAT`.

`fingerprint.DefaultContracts()` returns:

**ESP32:**
| Name | Regex | Severity |
|---|---|---|
| `esp32-panic` | `(?i)panic:\|Guru Meditation Error` | critical |
| `esp32-abort` | `abort\(\)` | critical |
| `esp32-brownout` | `(?i)brownout detector` | critical |
| `esp32-stackoverflow` | `(?i)stack overflow` | critical |
| `esp32-error` | `^E \(\d+\) ` | error |
| `esp32-warning` | `^W \(\d+\) ` | warning |

**NMEA (GPS):**
| Name | Regex | Severity |
|---|---|---|
| `nmea-invalid-fix` | `\$GP[A-Z]+,[^,]*,V,` | warning |
| `nmea-no-signal` | `\$GPGSA,A,1,` | warning |

**AT (GPRS/SIM):**
| Name | Regex | Severity |
|---|---|---|
| `at-error` | `^(ERROR\|+CME ERROR\|+CMS ERROR)` | error |
| `at-no-carrier` | `^(NO CARRIER\|NO DIALTONE\|BUSY)` | warning |
| `at-registration-denied` | `\+CREG: [04]` | error |

### New MCP Tool: `get_serial_events`

```
get_serial_events(vassal: string, since: string, severity: string) → []Event
```

Parameters:
- `vassal` — vassal name (must have `type: serial`)
- `since` — duration string: `"5m"`, `"1h"`, `"30s"`
- `severity` — `"warning"` | `"error"` | `"critical"` | `""` (all)

Returns JSON array of `store.Event` filtered by vassal source and time window.

This reuses the existing `store.ListEvents` with a time filter — no new DB schema needed.

---

## L: Machine-readable Logs

All structured log output uses `key=value` format (logfmt). No lore strings in code paths.

### Mapping

| Old (lore) | New (structured) |
|---|---|
| `🛡️ Royal Guard: Halt! Secret token spotted in config.yml` | `FILE_BLOCKED reason=AWS_KEY_DETECTED file=config.yml` |
| `⚖️ Inquisitor: Artifact passed integrity checks` | `ARTIFACT_REGISTERED name=firmware.bin version=3 checksum=sha256:a3f9` |
| `🔮 Court Mage: Go project detected — contracts inscribed` | `AUTO_INTEGRITY repo=./api type=go contracts=go-vet-error` |
| `📯 Herald: vassal down` | `VASSAL_FAILED name=tests exit=1 duration=4.2s` |
| `INVALID_SECURITY: filename:blacklisted:.env` | `FILE_BLOCKED reason=FILENAME_BLACKLISTED file=.env` |

### Error Code Registry

Standardized reason codes for `FILE_BLOCKED`:
- `FILENAME_BLACKLISTED` — exact filename match
- `EXTENSION_BLOCKED` — `.pem`, `.key`, etc.
- `AWS_KEY_DETECTED` — content regex
- `GITHUB_TOKEN_DETECTED`
- `PRIVATE_KEY_DETECTED`
- `SCANNER_REJECTED` — external scanner exit≠0

---

## P: Plugin Security Scanner

### Config

```yaml
# .king/kingdom.yml
settings:
  security_scanner: gitleaks           # binary name, must be in PATH
  security_scanner_args: ["--no-git"]  # optional extra args
```

### Behavior

When `security_scanner` is set, `Ledger.Register` calls:

```bash
<security_scanner> detect --source <file_path> [security_scanner_args...] --exit-code 1
```

- Exit 0 → pass, continue registration
- Exit non-0 → `FILE_BLOCKED reason=SCANNER_REJECTED scanner=<name> file=<path>`
- Timeout (5s) → log `SCANNER_TIMEOUT`, continue registration (fail-open)
- Binary not found → log `SCANNER_NOT_FOUND`, continue registration (fail-open)

The built-in regex scanner (`internal/security`) remains as the default when `security_scanner` is not set. It is never disabled — it runs first, then the external scanner if configured.

### Config Type

```go
type Settings struct {
    // existing fields ...
    SecurityScanner     string   `yaml:"security_scanner,omitempty"`
    SecurityScannerArgs []string `yaml:"security_scanner_args,omitempty"`
}
```

---

## R: P2P Registry

### File Location

`~/.king/registry.json` — global, per-user. Created on first `king up`.

### Schema

```json
{
  "/home/alex/projects/api": {
    "socket":  "/home/alex/projects/api/.king/king-a3f9c1.sock",
    "pid":     12345,
    "name":    "api",
    "updated": "2026-04-03T14:00:00Z"
  },
  "/home/alex/projects/firmware": {
    "socket":  "/home/alex/projects/firmware/.king/king-b7d2e4.sock",
    "pid":     67890,
    "name":    "firmware",
    "updated": "2026-04-03T13:45:00Z"
  }
}
```

### Operations

- **`king up`**: atomically adds/updates own entry (file lock via `flock`)
- **`king down`**: removes own entry
- **`king list`**: reads file, pings each socket (`net.Dial` with 200ms timeout), prints table
- **Stale cleanup**: entries with `pid` no longer alive are pruned on every `king up`

### `king list` Output

```
KINGDOM                         STATUS    PID     VASSALS  EVENTS (1h)
/home/alex/projects/api         running   12345   3/3      2 errors
/home/alex/projects/firmware    running   67890   1/1      0
/home/alex/projects/old-app     stale     —       —        —
```

### New Package

`internal/registry/registry.go` — `Register`, `Unregister`, `List`, `Prune` functions.
No daemon. Pure file I/O with flock.

---

## Files Changed / Created

| File | Change |
|---|---|
| `internal/config/types.go` | Add `SerialPort`, `BaudRate`, `SerialProtocol` to `VassalConfig`; `SecurityScanner`, `SecurityScannerArgs` to `Settings` |
| `internal/fingerprint/fingerprint.go` | Add `ProjectTypeESP32`, `ProjectTypeNMEA`, `ProjectTypeAT` |
| `internal/fingerprint/contracts.go` | Add ESP32, NMEA, AT contract sets |
| `internal/daemon/daemon.go` | Expand `type: serial` in `startVassals` |
| `internal/artifacts/ledger.go` | Call external scanner if configured |
| `internal/mcp/server.go` | Add `get_serial_events` tool |
| `internal/security/scanner.go` | Replace lore error strings with structured codes |
| `internal/daemon/auto_integrity.go` | Replace lore log lines with logfmt |
| `internal/registry/registry.go` | New: P2P registry CRUD |
| `cmd/king/main.go` | Add `list` subcommand |

---

## Testing Strategy

- `internal/fingerprint`: add `TestDefaultContracts_ESP32`, `_NMEA`, `_AT`
- `internal/registry`: `TestRegister_WritesEntry`, `TestList_PrunesStale`, `TestList_SkipsDeadSocket`
- `internal/artifacts`: `TestRegister_ExternalScanner_Blocked`, `TestRegister_ExternalScanner_NotFound_FailOpen`
- `internal/security`: `TestScan_ErrorCodes_Structured` — verify no lore strings in `Reason` field

---

## Out of Scope

- Actual ESP32 flashing (`esptool.py` integration) — Phase 2
- GPRS data connection management — Phase 2
- Cross-kingdom event routing (P2P events, not just list) — separate design
- TUI dashboard — separate design
