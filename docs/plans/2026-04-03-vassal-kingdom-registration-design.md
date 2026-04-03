# Vassal Kingdom Registration Design

**Date:** 2026-04-03
**Status:** Approved

## Goal

When `king-vassal` starts, it discovers all available kingdoms and either connects automatically (one kingdom) or asks the user to choose (multiple kingdoms). Vassals register themselves with their King, and `king list` shows vassals nested under their kingdoms.

## Requirements

1. `king-vassal` auto-connects when one kingdom found
2. `king-vassal` shows interactive selection when multiple kingdoms found (terminal mode)
3. `king-vassal` returns error when multiple kingdoms found in `--stdio` mode
4. Vassals register with King at startup (in-memory, no disk state)
5. `king list` shows vassals nested under each kingdom
6. `king up` creates `kingdom.yml` with `vassals: []` and a commented example

## Architecture

### 1. Discovery (`internal/discovery`)

Add `FindAllKingdomSockets(startDir string) ([]KingdomInfo, error)`:
- Walks up directory tree collecting all `.king/king-*.sock` files
- Reads global registry `~/.king/registry.json` and adds alive kingdoms not already found
- Returns `[]KingdomInfo{Name, RootDir, SocketPath}`

```go
type KingdomInfo struct {
    Name       string
    RootDir    string
    SocketPath string
}
```

### 2. Vassal Registration (`internal/daemon`)

Add to `Daemon` struct:
```go
vassals   map[string]VassalInfo
vassalsMu sync.RWMutex
```

```go
type VassalInfo struct {
    Name     string
    RepoPath string
    Socket   string
    PID      int
}
```

Two new JSON-RPC methods on the daemon socket:

- **`vassal.Register`** — called by vassal at startup
  - Request: `{name, repo_path, socket, pid}`
  - Response: `{ok: true}`

- **`vassal.List`** — called by `king list`
  - Request: `{}`
  - Response: `[{name, repo_path, socket, pid, alive}]`
  - Checks liveness via `Signal(0)` per entry

### 3. King-Vassal Startup (`cmd/king-vassal/main.go`)

Replace single `FindKingdomSocket` call with `FindAllKingdomSockets`:

```
0 kingdoms → error: "No Kingdom found. Run king up first."
1 kingdom  → use automatically (current behavior)
2+ kingdoms + isatty(stdin) → interactive selection prompt
2+ kingdoms + --stdio       → error: "multiple kingdoms found, use --king-sock"
```

Interactive prompt:
```
Found multiple kingdoms:
  1. FIX      (/Users/alex/Desktop/FIX)
  2. Backend  (/Users/alex/Desktop/Backend)

Select kingdom [1-2]:
```

After selection: connect to chosen King via JSON-RPC and call `vassal.Register`.

### 4. King List (`cmd/king/main.go`)

After displaying each alive kingdom, dial its socket and call `vassal.List`. Display results nested:

```
KINGDOM                           STATUS    PID
/Users/alex/Desktop/FIX           running   12345
  vassals:
    firmware  (/emwirs-esp32-firmware)  alive
    mobile    (/mobile-app)             dead
/Users/alex/Desktop/Backend       running   67890
  vassals:
    (none)
```

If `vassal.List` RPC fails (older King without the method) → silently skip vassals section.

### 5. Default Kingdom Config (`internal/config/config.go`)

`DefaultConfig()` writes `kingdom.yml` as a string template instead of `yaml.Marshal` to preserve comments:

```yaml
name: my-kingdom
vassals: []
# Example vassal:
# vassals:
#   - name: shell
#     command: $SHELL
#     autostart: true
patterns:
  - name: generic-error
    regex: '(?i)error|FAIL|panic:'
    severity: error
    summary_template: "Error detected in {vassal}: {match}"
settings:
  log_retention_days: 7
  ...
```

## Files Changed

| File | Change |
|------|--------|
| `internal/discovery/discovery.go` | Add `FindAllKingdomSockets` + `KingdomInfo` type |
| `internal/daemon/daemon.go` | Add `vassals` map + `vassalsMu` to Daemon struct |
| `internal/daemon/vassal_rpc.go` | New file: `vassal.Register` and `vassal.List` RPC handlers |
| `cmd/king-vassal/main.go` | Replace discovery call, add interactive selection, call Register |
| `cmd/king/main.go` | Update `king list` to show vassals nested |
| `internal/config/config.go` | Write `kingdom.yml` as string template |

## Non-Goals

- Persisting vassal state across King restarts (in-memory only)
- Automatic re-registration after King restart
- Vassal unregister on shutdown (King restart clears all anyway)
