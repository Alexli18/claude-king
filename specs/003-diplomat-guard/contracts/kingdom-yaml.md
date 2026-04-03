# Contract: kingdom.yml Guard Configuration

## Overview

Guards are configured per-vassal in `.king/kingdom.yml` under the `guards:` key.
All guards are opt-in — a vassal with no `guards:` key runs without any checks.

## Schema

```yaml
vassals:
  - name: string          # vassal identifier
    command: string       # startup command
    guards:               # optional list of guard checks
      - type: string      # REQUIRED: one of port_check | log_watch | data_rate | health_check
        interval: int     # seconds between checks; default: 10; min: 1
        threshold: int    # consecutive failures before circuit opens; default: 3; min: 1

        # --- port_check fields ---
        port: int         # TCP port number (1–65535); REQUIRED for port_check
        expect: string    # "open" (default) | "closed"

        # --- log_watch fields ---
        fail_on:          # REQUIRED for log_watch; list of patterns (string or regex)
          - string

        # --- data_rate fields ---
        min: string       # REQUIRED for data_rate; e.g. "100bps", "1.5kbps", "1mbps"

        # --- health_check fields ---
        exec: string      # REQUIRED for health_check; script path relative to kingdom root
        timeout: int      # script execution timeout in seconds; default: 10; min: 1
```

## Validation Errors

| Error | Cause |
|-------|-------|
| `unknown guard type "foo"` | `type` is not one of the four valid types |
| `port_check: port is required` | `port` field missing for `port_check` guard |
| `port_check: port must be 1–65535` | Invalid port number |
| `log_watch: fail_on must not be empty` | No patterns specified |
| `log_watch: invalid regex "foo(": ...` | Pattern fails to compile |
| `data_rate: min is required` | Missing threshold for `data_rate` guard |
| `data_rate: invalid format "foo" (expected e.g. "100bps")` | Bad min format |
| `health_check: exec is required` | Script path missing |
| `guard interval must be >= 1` | Non-positive interval |
| `guard threshold must be >= 1` | Non-positive threshold |

## Examples

### Minimal (single guard)

```yaml
vassals:
  - name: api-server
    command: ./api-server
    guards:
      - type: port_check
        port: 8080
```

### ESP32 Collector with multiple guards

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
        timeout: 15
        threshold: 3
```

### Port must be closed (firewall check)

```yaml
vassals:
  - name: secure-service
    command: ./secure-service
    guards:
      - type: port_check
        port: 3306
        expect: closed   # database port must NOT be exposed
```

## Notes

- Guards run independently of delegation state — they always run when the daemon is active
- Circuit breaker auto-recovers when the next check passes (consecutive_fails resets to 0)
- Multiple guards on the same vassal are evaluated independently; any open circuit blocks AI modifications
- `interval` is the time between the END of one check and the START of the next (not a fixed tick)
