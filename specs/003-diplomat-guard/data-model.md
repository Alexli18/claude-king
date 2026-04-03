# Data Model: King v2.1 — Guards

## Existing Entities (Delegation — Already Implemented)

### DelegationInfo (in-memory, `internal/daemon`)

```go
type DelegationInfo struct {
    SessionPID    int       // PID of the controlling AI session
    LastHeartbeat time.Time // Time of last heartbeat signal
}

// Map: vassal name → DelegationInfo
// Location: Daemon.delegatedVassals (sync.Mutex protected)
// Persistence: NONE — resets on daemon restart
```

**State transitions**:
- `absent` → `delegated`: `delegate_control` RPC called successfully
- `delegated` → `absent`: `delegate_release` called OR heartbeat stale for 30s
- `delegated` → `delegated` (same PID): `delegate_heartbeat` updates LastHeartbeat

---

## New Entities (Guards)

### GuardConfig (config layer, `internal/config`)

Added to `VassalConfig`:

```go
type GuardConfig struct {
    Type      string            `yaml:"type"`                // "port_check" | "log_watch" | "data_rate" | "health_check"
    Interval  int               `yaml:"interval,omitempty"` // seconds; default 10
    Threshold int               `yaml:"threshold,omitempty"` // circuit breaker N; default 3

    // port_check fields
    Port   int    `yaml:"port,omitempty"`   // TCP port to check
    Expect string `yaml:"expect,omitempty"` // "open" | "closed" (default "open")

    // log_watch fields
    FailOn []string `yaml:"fail_on,omitempty"` // list of strings/regexes that trigger failure

    // data_rate fields
    Min string `yaml:"min,omitempty"` // minimum bytes/sec threshold, e.g. "100bps", "1kbps"

    // health_check fields
    Exec    string `yaml:"exec,omitempty"`    // path to script (relative to kingdom root)
    Timeout int    `yaml:"timeout,omitempty"` // script timeout in seconds; default 10
}
```

`VassalConfig` extended with:
```go
Guards []GuardConfig `yaml:"guards,omitempty"`
```

---

### GuardState (runtime, `internal/daemon`)

In-memory tracking of guard health per vassal:

```go
type GuardState struct {
    VassalName      string
    GuardIndex      int       // index into VassalConfig.Guards slice
    GuardType       string
    ConsecutiveFails int
    LastCheckTime   time.Time
    LastResult      GuardResult
    CircuitOpen     bool      // true = circuit breaker triggered, AI blocked
}

type GuardResult struct {
    OK      bool
    Message string    // human-readable description
    CheckedAt time.Time
}
```

**State transitions**:
```
OK state:
  Pass → ConsecutiveFails = 0, CircuitOpen = false
  Fail → ConsecutiveFails++
         if ConsecutiveFails >= Threshold: CircuitOpen = true, emit notification

Circuit open:
  Pass → ConsecutiveFails = 0, CircuitOpen = false (auto-recovery)
  Fail → ConsecutiveFails++ (stays open)
```

**Storage**: In-memory only. Map key: `"vassalName:guardIndex"`.
- Location: `Daemon.guardStates map[string]*GuardState` (sync.RWMutex protected)
- Persistence: NONE — resets on daemon restart (guards re-run from scratch)

---

## Validation Rules

### GuardConfig validation (added to `config.Validate()`)

| Field | Rule |
|-------|------|
| `type` | Must be one of: `port_check`, `log_watch`, `data_rate`, `health_check` |
| `port_check.port` | Must be 1–65535 |
| `port_check.expect` | Must be `"open"` or `"closed"` if set; defaults to `"open"` |
| `log_watch.fail_on` | Must have at least one pattern; each pattern must compile as valid regex |
| `data_rate.min` | Must match pattern `^\d+(\.\d+)?(bps\|kbps\|mbps)$` (case-insensitive) |
| `health_check.exec` | Must not be empty; path must be non-empty string |
| `interval` | Must be ≥ 1 second if set; defaults to 10 |
| `threshold` | Must be ≥ 1 if set; defaults to 3 |
| `timeout` | Must be ≥ 1 if set; defaults to 10 (health_check only) |

---

## Kingdom Config Extension Example

```yaml
vassals:
  - name: esp32-collector
    command: python collector.py
    guards:
      - type: data_rate
        min: 100bps
        interval: 10
        threshold: 3
      - type: log_watch
        fail_on:
          - "SerialException"
          - "Panic"
        interval: 5
      - type: health_check
        exec: ./scripts/validate_packet_integrity.py
        timeout: 10
        threshold: 3

  - name: api
    command: ./api-server
    guards:
      - type: port_check
        port: 8080
        expect: open
        interval: 10
```

---

## Relationships

```
KingdomConfig
  └── VassalConfig[]
        └── GuardConfig[]      (config, persisted in kingdom.yml)
              ↓ (daemon reads at startup)
        GuardState[]           (runtime, in-memory only)
              ↓ (guard runner populates)
        GuardResult            (latest check outcome)
```

No database storage is needed — guard state is ephemeral by design (resets when daemon restarts, which is correct: a fresh daemon should re-evaluate health from scratch).
