# VMP Protocol: Vassal Manifest Protocol

**Date**: 2026-04-02 | **Version**: 1.0

The VMP protocol allows repositories to self-declare their capabilities as vassals within a Kingdom. A `vassal.json` file in the repository root describes what the vassal can do, what it produces, and what it needs.

## Schema

```json
{
  "$schema": "https://claude-king.dev/schemas/vassal.json",
  "name": "string (required)",
  "version": "string (required, semver)",
  "description": "string (optional)",
  "skills": [
    {
      "name": "string (required, identifier)",
      "command": "string (required, shell command)",
      "description": "string (required)",
      "inputs": ["string (artifact names)"],
      "outputs": ["string (artifact names)"]
    }
  ],
  "artifacts": [
    {
      "name": "string (required, unique identifier)",
      "path": "string (required, relative to repo root)",
      "description": "string (optional)",
      "mime_type": "string (optional)"
    }
  ],
  "dependencies": ["string (artifact names from other vassals)"],
  "config": {
    "autostart": "boolean (default: false)",
    "restart_policy": "never | on-failure | always (default: never)",
    "env": { "KEY": "VALUE" }
  }
}
```

## Example: ESP32 Firmware Project

```json
{
  "name": "esp32-firmware",
  "version": "1.0.0",
  "description": "ESP32 firmware builder and flash tool",
  "skills": [
    {
      "name": "build_firmware",
      "command": "idf.py build",
      "description": "Compile ESP32 firmware",
      "inputs": [],
      "outputs": ["firmware.bin"]
    },
    {
      "name": "flash_esp32",
      "command": "idf.py -p /dev/ttyUSB0 flash",
      "description": "Flash compiled firmware to connected ESP32",
      "inputs": ["firmware.bin"],
      "outputs": []
    },
    {
      "name": "monitor_serial",
      "command": "idf.py -p /dev/ttyUSB0 monitor",
      "description": "Start serial monitor for ESP32 output",
      "inputs": [],
      "outputs": []
    }
  ],
  "artifacts": [
    {
      "name": "firmware.bin",
      "path": "build/firmware.bin",
      "mime_type": "application/octet-stream"
    }
  ],
  "dependencies": [],
  "config": {
    "autostart": false,
    "restart_policy": "never",
    "env": {
      "IDF_PATH": "/opt/esp-idf"
    }
  }
}
```

## Example: Go API Server

```json
{
  "name": "api-server",
  "version": "2.1.0",
  "description": "Main backend API server",
  "skills": [
    {
      "name": "run_server",
      "command": "go run ./cmd/server",
      "description": "Start the API server in development mode",
      "inputs": [],
      "outputs": []
    },
    {
      "name": "run_tests",
      "command": "go test ./...",
      "description": "Run all tests",
      "inputs": [],
      "outputs": ["test-report.json"]
    }
  ],
  "artifacts": [
    {
      "name": "test-report.json",
      "path": "test-report.json",
      "mime_type": "application/json"
    }
  ],
  "dependencies": ["firmware.bin"],
  "config": {
    "autostart": true,
    "restart_policy": "on-failure",
    "env": {
      "PORT": "8080"
    }
  }
}
```

## Discovery

When `king up` is executed:
1. King scans `.king/kingdom.yml` for vassal definitions with explicit `repo_path` entries
2. For each repo_path, King checks for `vassal.json` and merges declared skills/artifacts into the vassal's registration
3. Skills become available as callable operations via `exec_skill(vassal, skill_name)` MCP tool
4. Artifacts are pre-registered in the Artifact Ledger with their declared paths
