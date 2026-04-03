# Quickstart: King v2.1 Guards

## Prerequisites

- King daemon running (`king up`)
- At least one vassal defined in `.king/kingdom.yml`

## Step 1: Add Guards to kingdom.yml

Edit `.king/kingdom.yml` and add `guards:` under any vassal:

```yaml
vassals:
  - name: my-server
    command: ./my-server
    guards:
      - type: port_check
        port: 8080
```

## Step 2: Restart the Daemon

Guards are loaded at daemon startup:

```bash
king down && king up
```

## Step 3: Check Guard Status

From any Claude Code session inside the kingdom:

```
Use the guard_status MCP tool to see the current guard health.
```

Or from the TUI — the health panel shows guard state in real time.

## Guard Types Reference

### port_check — Is a port open or closed?

```yaml
- type: port_check
  port: 8080          # port to check
  expect: open        # "open" (default) or "closed"
  interval: 10        # seconds between checks
  threshold: 3        # failures before AI is blocked
```

### log_watch — Watch for error patterns in output

```yaml
- type: log_watch
  fail_on:
    - "SerialException"   # plain string match
    - "Panic: .*"         # or regex
  interval: 5
  threshold: 3
```

### data_rate — Minimum data throughput

```yaml
- type: data_rate
  min: 100bps       # minimum bytes per second
                    # supports: bps, kbps, mbps
  interval: 10
  threshold: 3
```

### health_check — Custom validation script

```yaml
- type: health_check
  exec: ./scripts/validate.sh   # relative to kingdom root
  timeout: 10                    # seconds before script is killed
  interval: 10
  threshold: 3
```

The script must exit with code `0` for success, any non-zero for failure.

## How Circuit Breaker Works

1. Each guard tracks consecutive failures independently
2. When failures reach `threshold` (default: 3), the circuit **opens**
3. With an open circuit, AI sessions **cannot** take delegation control of that vassal
4. The circuit **auto-closes** when the next check passes (no manual reset needed)
5. User sees log message: `Guard 'data_rate' circuit open for vassal 'esp32-collector'. AI modifications blocked.`

## Multi-Agent Safety Example

```yaml
vassals:
  - name: api
    command: ./api-server
    guards:
      - type: port_check
        port: 8080

  - name: esp32-collector
    command: python collector.py
    guards:
      - type: data_rate
        min: 100bps
      - type: log_watch
        fail_on: ["SerialException", "Panic"]
```

With this config:
- Claude-Backend can take control of `api` only if port 8080 is responding
- Claude-Frontend cannot take control of `esp32-collector` if data rate is too low
- The King daemon guards physical hardware from AI hallucinations
