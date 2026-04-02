package store

import (
	"database/sql"
	"fmt"
)

// migration represents a single schema migration.
type migration struct {
	Version int
	SQL     string
}

// allMigrations is the ordered list of schema migrations.
var allMigrations = []migration{
	{
		Version: 1,
		SQL: `
CREATE TABLE IF NOT EXISTS kingdoms (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL UNIQUE,
    socket_path TEXT NOT NULL,
    pid INTEGER,
    status TEXT NOT NULL DEFAULT 'stopped',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS vassals (
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

CREATE TABLE IF NOT EXISTS artifacts (
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

CREATE TABLE IF NOT EXISTS events (
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

CREATE INDEX IF NOT EXISTS idx_events_kingdom_time ON events(kingdom_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_severity ON events(kingdom_id, severity);
CREATE INDEX IF NOT EXISTS idx_vassals_kingdom ON vassals(kingdom_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_kingdom ON artifacts(kingdom_id);
`,
	},
	{
		Version: 2,
		SQL: `
CREATE TABLE IF NOT EXISTS audit_entries (
    id TEXT PRIMARY KEY,
    kingdom_id TEXT NOT NULL REFERENCES kingdoms(id) ON DELETE CASCADE,
    layer TEXT NOT NULL CHECK(layer IN ('ingestion', 'sieve', 'action')),
    source TEXT NOT NULL,
    source_id TEXT,
    content TEXT NOT NULL,
    trace_id TEXT,
    metadata TEXT,
    sampled INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_audit_kingdom_time ON audit_entries(kingdom_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_kingdom_layer_time ON audit_entries(kingdom_id, layer, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_kingdom_source_time ON audit_entries(kingdom_id, source, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_trace_id ON audit_entries(trace_id);

CREATE TABLE IF NOT EXISTS action_traces (
    trace_id TEXT PRIMARY KEY,
    kingdom_id TEXT NOT NULL REFERENCES kingdoms(id) ON DELETE CASCADE,
    vassal_name TEXT NOT NULL,
    vassal_id TEXT,
    command TEXT NOT NULL,
    trigger_event_id TEXT,
    status TEXT NOT NULL CHECK(status IN ('running', 'completed', 'failed', 'timeout')),
    exit_code INTEGER,
    output TEXT,
    duration_ms INTEGER,
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_traces_kingdom_time ON action_traces(kingdom_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_vassal_time ON action_traces(vassal_name, started_at DESC);

CREATE TABLE IF NOT EXISTS approval_requests (
    id TEXT PRIMARY KEY,
    kingdom_id TEXT NOT NULL REFERENCES kingdoms(id) ON DELETE CASCADE,
    trace_id TEXT NOT NULL,
    command TEXT NOT NULL,
    vassal_name TEXT NOT NULL,
    reason TEXT,
    status TEXT NOT NULL CHECK(status IN ('pending', 'approved', 'rejected', 'expired', 'timeout')),
    responded_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_approvals_kingdom_status ON approval_requests(kingdom_id, status);
CREATE INDEX IF NOT EXISTS idx_approvals_kingdom_time ON approval_requests(kingdom_id, created_at DESC);
`,
	},
}

// RunMigrations creates the schema_version tracking table and applies any
// pending migrations in order.
func RunMigrations(db *sql.DB) error {
	// Ensure the version-tracking table exists.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Determine the current schema version.
	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("query schema version: %w", err)
	}

	for _, m := range allMigrations {
		if m.Version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute migration %d: %w", m.Version, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", m.Version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}

	return nil
}
