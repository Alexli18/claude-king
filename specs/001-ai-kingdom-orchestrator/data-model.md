# Data Model: Claude King

**Date**: 2026-04-02 | **Status**: Draft

## Entities

### Kingdom

The top-level orchestration environment, scoped to a project directory.

| Field | Type | Description |
|-------|------|-------------|
| id | string (UUID) | Unique Kingdom identifier |
| name | string | Human-readable name (defaults to directory name) |
| root_path | string | Absolute path to project directory |
| socket_path | string | Path to Unix Domain Socket (`.king/king.sock`) |
| pid | int | Daemon process ID |
| status | enum | `starting`, `running`, `stopping`, `stopped`, `crashed` |
| created_at | timestamp | Kingdom creation time |
| updated_at | timestamp | Last state change |

**Relationships**: Has many Vassals, has many Artifacts, has many Events.

### Vassal

A managed PTY session within a Kingdom.

| Field | Type | Description |
|-------|------|-------------|
| id | string (UUID) | Unique Vassal identifier |
| kingdom_id | string (FK) | Parent Kingdom |
| name | string | Human-readable name (e.g., "esp32-monitor", "api-server") |
| command | string | Shell command running in PTY |
| status | enum | `idle`, `running`, `error`, `terminated` |
| pid | int | PTY child process ID |
| pty_fd | int | PTY file descriptor (runtime only, not persisted) |
| skills | []string | Declared skills from `vassal.json` |
| artifacts | []string | Declared artifact types from `vassal.json` |
| created_at | timestamp | Session creation time |
| last_activity | timestamp | Last output received |

**State transitions**:
```
idle → running (command started)
running → idle (command completed successfully)
running → error (command exited with error)
running → terminated (explicitly killed)
error → running (restarted)
terminated → running (restarted)
```

### Artifact

A file or build product registered in the Artifact Ledger.

| Field | Type | Description |
|-------|------|-------------|
| id | string (UUID) | Unique Artifact identifier |
| kingdom_id | string (FK) | Parent Kingdom |
| producer_id | string (FK) | Vassal that produced this artifact |
| name | string | Short name (used in `king://artifacts/{name}`) |
| file_path | string | Absolute path to actual file |
| mime_type | string | File MIME type |
| version | int | Auto-incrementing version (updates on re-register) |
| checksum | string | SHA-256 of file content |
| created_at | timestamp | First registration time |
| updated_at | timestamp | Last update time |

**Validation**: `name` must be unique within a Kingdom. `file_path` must exist on disk when registered.

### Event

A significant occurrence detected in vassal output.

| Field | Type | Description |
|-------|------|-------------|
| id | string (UUID) | Unique Event identifier |
| kingdom_id | string (FK) | Parent Kingdom |
| source_id | string (FK) | Vassal that produced the event |
| severity | enum | `info`, `warning`, `error`, `critical` |
| pattern | string | Pattern rule that triggered detection |
| summary | string | Human-readable event summary |
| raw_output | string | Original output lines that triggered the event |
| correlation | string | Optional correlation context (e.g., related commit) |
| acknowledged | bool | Whether user has seen/acted on this event |
| created_at | timestamp | Event detection time |

### KingdomConfig

Configuration loaded from `.king/kingdom.yml`.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Kingdom display name |
| vassals | []VassalConfig | Vassal definitions |
| patterns | []PatternConfig | Event detection patterns |
| artifacts_dir | string | Override for artifact storage path |

### VassalConfig

Vassal definition within Kingdom config.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Vassal identifier |
| command | string | Shell command to run |
| cwd | string | Working directory (relative to Kingdom root) |
| env | map[string]string | Additional environment variables |
| autostart | bool | Start automatically on `king up` |
| restart_policy | enum | `never`, `on-failure`, `always` |

### VassalManifest (VMP Protocol)

Contents of `vassal.json` in a repository.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Vassal display name |
| version | string | Manifest version |
| skills | []Skill | Declared capabilities |
| artifacts | []ArtifactDecl | Declared artifact outputs |
| dependencies | []string | Required artifacts from other vassals |

### Skill

A declared capability in VMP.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Skill identifier (e.g., `flash_esp32`) |
| command | string | Shell command implementing the skill |
| description | string | Human-readable description |
| inputs | []string | Required input artifact names |
| outputs | []string | Produced artifact names |

## SQLite Schema

```sql
CREATE TABLE kingdoms (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL UNIQUE,
    socket_path TEXT NOT NULL,
    pid INTEGER,
    status TEXT NOT NULL DEFAULT 'stopped',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE vassals (
    id TEXT PRIMARY KEY,
    kingdom_id TEXT NOT NULL REFERENCES kingdoms(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'idle',
    pid INTEGER,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_activity TEXT,
    UNIQUE(kingdom_id, name)
);

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    kingdom_id TEXT NOT NULL REFERENCES kingdoms(id) ON DELETE CASCADE,
    producer_id TEXT REFERENCES vassals(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    mime_type TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    checksum TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(kingdom_id, name)
);

CREATE TABLE events (
    id TEXT PRIMARY KEY,
    kingdom_id TEXT NOT NULL REFERENCES kingdoms(id) ON DELETE CASCADE,
    source_id TEXT REFERENCES vassals(id) ON DELETE SET NULL,
    severity TEXT NOT NULL,
    pattern TEXT,
    summary TEXT NOT NULL,
    raw_output TEXT,
    correlation TEXT,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_events_kingdom_time ON events(kingdom_id, created_at DESC);
CREATE INDEX idx_events_severity ON events(kingdom_id, severity);
CREATE INDEX idx_vassals_kingdom ON vassals(kingdom_id);
CREATE INDEX idx_artifacts_kingdom ON artifacts(kingdom_id);
```
