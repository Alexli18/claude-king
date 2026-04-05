# Kingdom Configuration Reference

## kingdom.yml

Full example with all vassal types:

```yaml
name: MyKingdom

vassals:
  # Shell vassal — command with PTY
  - name: api
    command: npm run dev
    autostart: true
    restart_policy: always   # always | on-failure | no (default: always)

  # Claude Code sub-agent vassal
  - name: firmware
    type: claude
    repo_path: emwirs-esp32-firmware   # relative to kingdom root
    autostart: true

  # Serial device vassal (ESP32, Arduino, GPS)
  - name: sensor
    type: serial
    serial_port: /dev/tty.usbserial-0001   # or "auto:esp32", "auto:ftdi", "auto:gps", "auto:any"
    baud_rate: 115200
    autostart: true

patterns:
  - name: generic-error
    regex: '(?i)error|FAIL|panic:'
    severity: error
    summary_template: "Error detected in {vassal}: {match}"

  - name: build-success
    regex: 'Build successful|compiled successfully'
    severity: info
    summary_template: "Build complete in {vassal}"

settings:
  log_retention_days: 7
  max_output_buffer: 10MB
  event_cooldown_seconds: 30
  audit_retention_days: 7
  audit_ingestion_retention_days: 1
  sovereign_approval_timeout: 300
  audit_max_trace_output: 10000
  sovereign_approval: false   # set true to require approval before exec_in
```

## .mcp.json

For Claude Code in the kingdom root directory:

```json
{
  "mcpServers": {
    "king": {
      "command": "/path/to/king",
      "args": ["mcp"],
      "cwd": "/path/to/kingdom-root"
    },
    "firmware": {
      "command": "/path/to/king-vassal",
      "args": ["--stdio", "--name", "firmware"],
      "cwd": "/path/to/kingdom-root"
    },
    "ml-pipeline": {
      "command": "/path/to/king-vassal",
      "args": ["--stdio", "--name", "ml-pipeline"],
      "cwd": "/path/to/kingdom-root"
    }
  }
}
```

**Notes:**
- `king mcp` starts the MCP gateway; if daemon already running it attaches to it
- `--name` must match the vassal name in `kingdom.yml`; king-vassal auto-resolves `repo_path` from the config
- `cwd` should be the kingdom root (not the sub-repo); Claude Code may not honor per-vassal `cwd`
- Daemon must be running (`king up --detach`) before opening Claude Code

## vassal.json (optional per-repo manifest)

Place in each sub-repo root for auto-configuration:

```json
{
  "name": "firmware",
  "type": "esp32",
  "build_command": "idf.py build",
  "test_command": "pytest tests/"
}
```

## Vassal Config Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique vassal identifier |
| `type` | string | `shell` (default), `claude`, `serial` |
| `command` | string | Shell command (type:shell only) |
| `repo_path` | string | Sub-repo path relative to kingdom root (type:claude) |
| `serial_port` | string | Device path or `auto:<hint>` (type:serial) |
| `baud_rate` | int | Serial baud rate (default: 115200) |
| `autostart` | bool | Start on daemon startup |
| `restart_policy` | string | `always`, `on-failure`, `no` |
| `env` | map | Environment variables |

## Settings Fields

| Field | Default | Description |
|-------|---------|-------------|
| `log_retention_days` | 7 | Days to keep event log |
| `max_output_buffer` | 10MB | Max PTY output buffer |
| `event_cooldown_seconds` | 30 | Min seconds between same-pattern events |
| `audit_retention_days` | 7 | Days to keep audit entries |
| `sovereign_approval` | false | Require approval for exec_in calls |
| `sovereign_approval_timeout` | 300 | Seconds to wait for approval |
