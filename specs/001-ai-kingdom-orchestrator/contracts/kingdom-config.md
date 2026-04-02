# Kingdom Configuration: `.king/kingdom.yml`

**Date**: 2026-04-02 | **Version**: 1.0

## Schema

```yaml
# .king/kingdom.yml
name: string                    # Kingdom display name (default: directory name)

vassals:
  - name: string                # Unique vassal identifier (required)
    command: string             # Shell command to run (required)
    cwd: string                 # Working directory, relative to kingdom root (default: ".")
    repo_path: string           # Path to vassal's repository (for VMP discovery)
    env:                        # Additional environment variables
      KEY: VALUE
    autostart: bool             # Start on `king up` (default: true)
    restart_policy: string      # never | on-failure | always (default: never)

patterns:                       # Semantic Sieve event detection rules
  - name: string                # Pattern identifier (required)
    regex: string               # Regular expression to match (required)
    severity: string            # info | warning | error | critical (required)
    source: string              # Vassal name filter (optional, default: all)
    summary_template: string    # Template for event summary (optional)
                                # Supports {match}, {group.N}, {vassal} placeholders

artifacts_dir: string           # Override artifact storage (default: .king/artifacts/)

settings:
  log_retention_days: int       # Days to keep event logs (default: 7)
  max_output_buffer: string     # Per-vassal output buffer size (default: "10MB")
  event_cooldown_seconds: int   # Min seconds between duplicate events (default: 30)
```

## Example

```yaml
name: smart-home-project

vassals:
  - name: api-server
    command: go run ./cmd/server
    cwd: .
    autostart: true
    restart_policy: on-failure
    env:
      PORT: "8080"
      DEBUG: "true"

  - name: esp32-monitor
    command: minicom -D /dev/ttyUSB0 -b 115200
    repo_path: ../esp32-firmware
    autostart: true
    restart_policy: always

  - name: frontend-dev
    command: npm run dev
    cwd: ../web-dashboard
    autostart: true
    env:
      VITE_API_URL: "http://localhost:8080"

  - name: log-watcher
    command: tail -f /var/log/syslog
    autostart: false

patterns:
  - name: esp32-error
    regex: "E \\(\\d+\\) .+: (.+)"
    severity: error
    source: esp32-monitor
    summary_template: "ESP32 error: {group.1}"

  - name: panic-detected
    regex: "panic:|FATAL|Segmentation fault"
    severity: critical
    summary_template: "Critical failure in {vassal}: {match}"

  - name: build-failure
    regex: "FAIL|Build failed|error\\[E"
    severity: error
    summary_template: "Build failure in {vassal}: {match}"

settings:
  log_retention_days: 14
  max_output_buffer: "20MB"
  event_cooldown_seconds: 10
```

## Default Configuration

When `king up` is run in a directory without `.king/kingdom.yml`, the system creates:

```yaml
name: <directory-name>

vassals:
  - name: shell
    command: $SHELL
    autostart: true

patterns:
  - name: generic-error
    regex: "[Ee]rror|FAIL|panic:"
    severity: error
    summary_template: "Error detected in {vassal}: {match}"

settings:
  log_retention_days: 7
  max_output_buffer: "10MB"
  event_cooldown_seconds: 30
```
