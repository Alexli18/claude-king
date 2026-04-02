package store

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver.
)

// ---------------------------------------------------------------------------
// Entity types
// ---------------------------------------------------------------------------

// Kingdom represents a managed workspace.
type Kingdom struct {
	ID         string
	Name       string
	RootPath   string
	SocketPath string
	PID        int
	Status     string
	CreatedAt  string
	UpdatedAt  string
}

// Vassal represents a child process managed within a kingdom.
type Vassal struct {
	ID           string
	KingdomID    string
	Name         string
	Command      string
	Status       string
	PID          int
	CreatedAt    string
	LastActivity string
}

// Artifact represents a file-based output produced by a vassal.
type Artifact struct {
	ID         string
	KingdomID  string
	ProducerID string
	Name       string
	FilePath   string
	MimeType   string
	Version    int
	Checksum   string
	CreatedAt  string
	UpdatedAt  string
}

// Event represents a notable occurrence within a kingdom.
type Event struct {
	ID           string
	KingdomID    string
	SourceID     string
	Severity     string
	Pattern      string
	Summary      string
	RawOutput    string
	Correlation  string
	Acknowledged bool
	CreatedAt    string
}

// AuditEntry represents a single audit record across any of the three layers.
type AuditEntry struct {
	ID        string
	KingdomID string
	Layer     string // "ingestion", "sieve", "action"
	Source    string
	SourceID  string
	Content   string
	TraceID   string
	Metadata  string // JSON
	Sampled   bool
	CreatedAt string
}

// ActionTrace represents a detailed trace of an exec_in command execution.
type ActionTrace struct {
	TraceID        string
	KingdomID      string
	VassalName     string
	VassalID       string
	Command        string
	TriggerEventID string
	Status         string // "running", "completed", "failed", "timeout"
	ExitCode       int
	Output         string
	DurationMs     int
	StartedAt      string
	CompletedAt    string
}

// ApprovalRequest represents a Sovereign Approval request for an exec_in command.
type ApprovalRequest struct {
	ID         string
	KingdomID  string
	TraceID    string
	Command    string
	VassalName string
	Reason     string
	Status     string // "pending", "approved", "rejected", "expired", "timeout"
	RespondedAt string
	CreatedAt  string
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store wraps a SQLite database connection and provides CRUD helpers.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at dbPath, enables WAL mode
// and foreign-key enforcement, and runs any pending schema migrations.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Enable foreign-key constraint enforcement.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := RunMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB, useful for tests or advanced queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ---------------------------------------------------------------------------
// Kingdom CRUD
// ---------------------------------------------------------------------------

// CreateKingdom inserts a new kingdom row.
func (s *Store) CreateKingdom(k Kingdom) error {
	_, err := s.db.Exec(`
		INSERT INTO kingdoms (id, name, root_path, socket_path, pid, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.RootPath, k.SocketPath, nullableInt(k.PID), k.Status, k.CreatedAt, k.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert kingdom: %w", err)
	}
	return nil
}

// GetKingdom retrieves a kingdom by its primary key.
func (s *Store) GetKingdom(id string) (*Kingdom, error) {
	row := s.db.QueryRow(`
		SELECT id, name, root_path, socket_path, pid, status, created_at, updated_at
		FROM kingdoms WHERE id = ?`, id)
	return scanKingdom(row)
}

// GetKingdomByPath retrieves a kingdom by its unique root_path.
func (s *Store) GetKingdomByPath(rootPath string) (*Kingdom, error) {
	row := s.db.QueryRow(`
		SELECT id, name, root_path, socket_path, pid, status, created_at, updated_at
		FROM kingdoms WHERE root_path = ?`, rootPath)
	return scanKingdom(row)
}

// UpdateKingdomStatus sets the status and bumps updated_at.
func (s *Store) UpdateKingdomStatus(id, status string) error {
	res, err := s.db.Exec(`
		UPDATE kingdoms SET status = ?, updated_at = datetime('now') WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update kingdom status: %w", err)
	}
	return ensureRowAffected(res, "kingdom", id)
}

// UpdateKingdomPID sets the process ID and bumps updated_at.
func (s *Store) UpdateKingdomPID(id string, pid int) error {
	res, err := s.db.Exec(`
		UPDATE kingdoms SET pid = ?, updated_at = datetime('now') WHERE id = ?`, nullableInt(pid), id)
	if err != nil {
		return fmt.Errorf("update kingdom pid: %w", err)
	}
	return ensureRowAffected(res, "kingdom", id)
}

// ---------------------------------------------------------------------------
// Vassal CRUD
// ---------------------------------------------------------------------------

// CreateVassal inserts a new vassal row.
func (s *Store) CreateVassal(v Vassal) error {
	_, err := s.db.Exec(`
		INSERT INTO vassals (id, kingdom_id, name, command, status, pid, created_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.KingdomID, v.Name, v.Command, v.Status, nullableInt(v.PID), v.CreatedAt, nullableString(v.LastActivity),
	)
	if err != nil {
		return fmt.Errorf("insert vassal: %w", err)
	}
	return nil
}

// GetVassal retrieves a vassal by primary key.
func (s *Store) GetVassal(id string) (*Vassal, error) {
	row := s.db.QueryRow(`
		SELECT id, kingdom_id, name, command, status, pid, created_at, last_activity
		FROM vassals WHERE id = ?`, id)
	return scanVassal(row)
}

// GetVassalByName retrieves a vassal by its unique (kingdom_id, name) pair.
func (s *Store) GetVassalByName(kingdomID, name string) (*Vassal, error) {
	row := s.db.QueryRow(`
		SELECT id, kingdom_id, name, command, status, pid, created_at, last_activity
		FROM vassals WHERE kingdom_id = ? AND name = ?`, kingdomID, name)
	return scanVassal(row)
}

// ListVassals returns all vassals belonging to a kingdom.
func (s *Store) ListVassals(kingdomID string) ([]Vassal, error) {
	rows, err := s.db.Query(`
		SELECT id, kingdom_id, name, command, status, pid, created_at, last_activity
		FROM vassals WHERE kingdom_id = ? ORDER BY created_at`, kingdomID)
	if err != nil {
		return nil, fmt.Errorf("list vassals: %w", err)
	}
	defer rows.Close()

	var vassals []Vassal
	for rows.Next() {
		v, err := scanVassalRow(rows)
		if err != nil {
			return nil, err
		}
		vassals = append(vassals, *v)
	}
	return vassals, rows.Err()
}

// UpdateVassalStatus sets the vassal status and updates last_activity.
func (s *Store) UpdateVassalStatus(id, status string) error {
	res, err := s.db.Exec(`
		UPDATE vassals SET status = ?, last_activity = datetime('now') WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update vassal status: %w", err)
	}
	return ensureRowAffected(res, "vassal", id)
}

// UpdateVassalPID sets the vassal process ID.
func (s *Store) UpdateVassalPID(id string, pid int) error {
	res, err := s.db.Exec(`
		UPDATE vassals SET pid = ? WHERE id = ?`, nullableInt(pid), id)
	if err != nil {
		return fmt.Errorf("update vassal pid: %w", err)
	}
	return ensureRowAffected(res, "vassal", id)
}

// DeleteVassal removes a vassal row by ID.
func (s *Store) DeleteVassal(id string) error {
	res, err := s.db.Exec("DELETE FROM vassals WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete vassal: %w", err)
	}
	return ensureRowAffected(res, "vassal", id)
}

// ---------------------------------------------------------------------------
// Artifact CRUD
// ---------------------------------------------------------------------------

// CreateArtifact inserts a new artifact row.
func (s *Store) CreateArtifact(a Artifact) error {
	_, err := s.db.Exec(`
		INSERT INTO artifacts (id, kingdom_id, producer_id, name, file_path, mime_type, version, checksum, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.KingdomID, nullableString(a.ProducerID), a.Name, a.FilePath,
		nullableString(a.MimeType), a.Version, nullableString(a.Checksum), a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert artifact: %w", err)
	}
	return nil
}

// GetArtifactByName retrieves an artifact by its unique (kingdom_id, name).
func (s *Store) GetArtifactByName(kingdomID, name string) (*Artifact, error) {
	row := s.db.QueryRow(`
		SELECT id, kingdom_id, producer_id, name, file_path, mime_type, version, checksum, created_at, updated_at
		FROM artifacts WHERE kingdom_id = ? AND name = ?`, kingdomID, name)
	return scanArtifact(row)
}

// ListArtifacts returns all artifacts belonging to a kingdom.
func (s *Store) ListArtifacts(kingdomID string) ([]Artifact, error) {
	rows, err := s.db.Query(`
		SELECT id, kingdom_id, producer_id, name, file_path, mime_type, version, checksum, created_at, updated_at
		FROM artifacts WHERE kingdom_id = ? ORDER BY created_at`, kingdomID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []Artifact
	for rows.Next() {
		a, err := scanArtifactRow(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, *a)
	}
	return artifacts, rows.Err()
}

// UpdateArtifact updates an existing artifact (matched by ID), bumping version
// and updated_at.
func (s *Store) UpdateArtifact(a Artifact) error {
	res, err := s.db.Exec(`
		UPDATE artifacts
		SET producer_id = ?, name = ?, file_path = ?, mime_type = ?, version = ?, checksum = ?, updated_at = datetime('now')
		WHERE id = ?`,
		nullableString(a.ProducerID), a.Name, a.FilePath,
		nullableString(a.MimeType), a.Version, nullableString(a.Checksum), a.ID,
	)
	if err != nil {
		return fmt.Errorf("update artifact: %w", err)
	}
	return ensureRowAffected(res, "artifact", a.ID)
}

// ---------------------------------------------------------------------------
// Event operations
// ---------------------------------------------------------------------------

// CreateEvent inserts a new event row. If the disk is full, the error is
// logged as a warning and the operation degrades gracefully instead of
// returning a hard error (T056).
func (s *Store) CreateEvent(e Event) error {
	ack := 0
	if e.Acknowledged {
		ack = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO events (id, kingdom_id, source_id, severity, pattern, summary, raw_output, correlation, acknowledged, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.KingdomID, nullableString(e.SourceID), e.Severity,
		nullableString(e.Pattern), e.Summary, nullableString(e.RawOutput),
		nullableString(e.Correlation), ack, e.CreatedAt,
	)
	if err != nil {
		if isDiskFull(err) {
			slog.Warn("disk full: event not persisted", "event_id", e.ID, "summary", e.Summary)
			return nil
		}
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// isDiskFull checks if an error indicates the disk is full.
// It detects both the SQLite FULL error (typically "database or disk is full")
// and the OS-level ENOSPC error.
func isDiskFull(err error) bool {
	if err == nil {
		return false
	}

	// Check for OS-level ENOSPC.
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if errno == syscall.ENOSPC {
			return true
		}
	}

	// Check for SQLite "database or disk is full" message.
	msg := err.Error()
	if strings.Contains(msg, "database or disk is full") || strings.Contains(msg, "SQLITE_FULL") {
		return true
	}

	return false
}

// ListEvents returns events for a kingdom with optional severity and source
// filters. Pass empty strings to omit filters. Limit controls the maximum
// number of rows returned (0 means no limit).
func (s *Store) ListEvents(kingdomID string, severity string, source string, limit int) ([]Event, error) {
	var (
		clauses []string
		args    []any
	)

	clauses = append(clauses, "kingdom_id = ?")
	args = append(args, kingdomID)

	if severity != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, severity)
	}
	if source != "" {
		clauses = append(clauses, "source_id = ?")
		args = append(args, source)
	}

	query := "SELECT id, kingdom_id, source_id, severity, pattern, summary, raw_output, correlation, acknowledged, created_at FROM events"
	query += " WHERE " + strings.Join(clauses, " AND ")
	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEventRow(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *e)
	}
	return events, rows.Err()
}

// AcknowledgeEvent marks an event as acknowledged.
func (s *Store) AcknowledgeEvent(id string) error {
	res, err := s.db.Exec("UPDATE events SET acknowledged = 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("acknowledge event: %w", err)
	}
	return ensureRowAffected(res, "event", id)
}

// DeleteOldEvents removes events older than retentionDays for a kingdom.
func (s *Store) DeleteOldEvents(kingdomID string, retentionDays int) error {
	_, err := s.db.Exec(`
		DELETE FROM events
		WHERE kingdom_id = ? AND created_at < datetime('now', ?)`,
		kingdomID, fmt.Sprintf("-%d days", retentionDays),
	)
	if err != nil {
		return fmt.Errorf("delete old events: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AuditEntry operations
// ---------------------------------------------------------------------------

// CreateAuditEntry inserts a single audit entry. Gracefully degrades on disk full.
func (s *Store) CreateAuditEntry(e AuditEntry) error {
	sampled := 0
	if e.Sampled {
		sampled = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO audit_entries (id, kingdom_id, layer, source, source_id, content, trace_id, metadata, sampled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.KingdomID, e.Layer, e.Source, nullableString(e.SourceID),
		e.Content, nullableString(e.TraceID), nullableString(e.Metadata),
		sampled, e.CreatedAt,
	)
	if err != nil {
		if isDiskFull(err) {
			slog.Warn("disk full: audit entry not persisted", "id", e.ID, "layer", e.Layer)
			return nil
		}
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

// CreateAuditEntryBatch inserts multiple audit entries in a single transaction.
func (s *Store) CreateAuditEntryBatch(entries []AuditEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin batch insert: %w", err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO audit_entries (id, kingdom_id, layer, source, source_id, content, trace_id, metadata, sampled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare batch insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		sampled := 0
		if e.Sampled {
			sampled = 1
		}
		_, err := stmt.Exec(
			e.ID, e.KingdomID, e.Layer, e.Source, nullableString(e.SourceID),
			e.Content, nullableString(e.TraceID), nullableString(e.Metadata),
			sampled, e.CreatedAt,
		)
		if err != nil {
			_ = tx.Rollback()
			if isDiskFull(err) {
				slog.Warn("disk full: audit batch not persisted", "count", len(entries))
				return nil
			}
			return fmt.Errorf("batch insert audit entry: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch insert: %w", err)
	}
	return nil
}

// AuditFilter holds optional filters for ListAuditEntries.
type AuditFilter struct {
	KingdomID string
	Layer     string
	Source    string
	Since     string // datetime string
	Until     string // datetime string
	TraceID   string
	Limit     int
}

// ListAuditEntries returns audit entries matching the given filters.
func (s *Store) ListAuditEntries(f AuditFilter) ([]AuditEntry, error) {
	var clauses []string
	var args []any

	clauses = append(clauses, "kingdom_id = ?")
	args = append(args, f.KingdomID)

	if f.Layer != "" {
		clauses = append(clauses, "layer = ?")
		args = append(args, f.Layer)
	}
	if f.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, f.Source)
	}
	if f.Since != "" {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, f.Since)
	}
	if f.Until != "" {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, f.Until)
	}
	if f.TraceID != "" {
		clauses = append(clauses, "trace_id = ?")
		args = append(args, f.TraceID)
	}

	query := "SELECT id, kingdom_id, layer, source, source_id, content, trace_id, metadata, sampled, created_at FROM audit_entries"
	query += " WHERE " + strings.Join(clauses, " AND ")
	query += " ORDER BY created_at DESC"

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		e, err := scanAuditEntryRow(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *e)
	}
	return entries, rows.Err()
}

// CountAuditEntries returns total count of audit entries matching the filter (for pagination info).
func (s *Store) CountAuditEntries(f AuditFilter) (int, error) {
	var clauses []string
	var args []any

	clauses = append(clauses, "kingdom_id = ?")
	args = append(args, f.KingdomID)

	if f.Layer != "" {
		clauses = append(clauses, "layer = ?")
		args = append(args, f.Layer)
	}
	if f.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, f.Source)
	}
	if f.Since != "" {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, f.Since)
	}
	if f.Until != "" {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, f.Until)
	}
	if f.TraceID != "" {
		clauses = append(clauses, "trace_id = ?")
		args = append(args, f.TraceID)
	}

	query := "SELECT COUNT(*) FROM audit_entries WHERE " + strings.Join(clauses, " AND ")
	var count int
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count audit entries: %w", err)
	}
	return count, nil
}

// DeleteOldAuditEntries removes audit entries older than retentionDays,
// with separate retention for ingestion vs sieve/action layers.
func (s *Store) DeleteOldAuditEntries(kingdomID string, ingestionRetentionDays, otherRetentionDays int) error {
	_, err := s.db.Exec(`
		DELETE FROM audit_entries
		WHERE kingdom_id = ? AND layer = 'ingestion' AND created_at < datetime('now', ?)`,
		kingdomID, fmt.Sprintf("-%d days", ingestionRetentionDays),
	)
	if err != nil {
		return fmt.Errorf("delete old ingestion entries: %w", err)
	}

	_, err = s.db.Exec(`
		DELETE FROM audit_entries
		WHERE kingdom_id = ? AND layer != 'ingestion' AND created_at < datetime('now', ?)`,
		kingdomID, fmt.Sprintf("-%d days", otherRetentionDays),
	)
	if err != nil {
		return fmt.Errorf("delete old audit entries: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ActionTrace operations
// ---------------------------------------------------------------------------

// CreateActionTrace inserts a new action trace record.
func (s *Store) CreateActionTrace(t ActionTrace) error {
	_, err := s.db.Exec(`
		INSERT INTO action_traces (trace_id, kingdom_id, vassal_name, vassal_id, command, trigger_event_id, status, exit_code, output, duration_ms, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TraceID, t.KingdomID, t.VassalName, nullableString(t.VassalID),
		t.Command, nullableString(t.TriggerEventID), t.Status,
		nullableInt(t.ExitCode), nullableString(t.Output), nullableInt(t.DurationMs),
		t.StartedAt, nullableString(t.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("insert action trace: %w", err)
	}
	return nil
}

// GetActionTrace retrieves an action trace by its trace_id.
func (s *Store) GetActionTrace(traceID string) (*ActionTrace, error) {
	row := s.db.QueryRow(`
		SELECT trace_id, kingdom_id, vassal_name, vassal_id, command, trigger_event_id, status, exit_code, output, duration_ms, started_at, completed_at
		FROM action_traces WHERE trace_id = ?`, traceID)
	return scanActionTrace(row)
}

// UpdateActionTrace updates the mutable fields of an action trace (status, exit_code, output, duration_ms, completed_at).
func (s *Store) UpdateActionTrace(t ActionTrace) error {
	res, err := s.db.Exec(`
		UPDATE action_traces
		SET status = ?, exit_code = ?, output = ?, duration_ms = ?, completed_at = ?
		WHERE trace_id = ?`,
		t.Status, nullableInt(t.ExitCode), nullableString(t.Output),
		nullableInt(t.DurationMs), nullableString(t.CompletedAt), t.TraceID,
	)
	if err != nil {
		return fmt.Errorf("update action trace: %w", err)
	}
	return ensureRowAffected(res, "action_trace", t.TraceID)
}

// ListActionTraces returns action traces for a kingdom, ordered by started_at DESC.
func (s *Store) ListActionTraces(kingdomID string, limit int) ([]ActionTrace, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT trace_id, kingdom_id, vassal_name, vassal_id, command, trigger_event_id, status, exit_code, output, duration_ms, started_at, completed_at
		FROM action_traces WHERE kingdom_id = ? ORDER BY started_at DESC LIMIT ?`, kingdomID, limit)
	if err != nil {
		return nil, fmt.Errorf("list action traces: %w", err)
	}
	defer rows.Close()

	var traces []ActionTrace
	for rows.Next() {
		t, err := scanActionTraceRow(rows)
		if err != nil {
			return nil, err
		}
		traces = append(traces, *t)
	}
	return traces, rows.Err()
}

// ---------------------------------------------------------------------------
// ApprovalRequest operations
// ---------------------------------------------------------------------------

// CreateApprovalRequest inserts a new approval request.
func (s *Store) CreateApprovalRequest(r ApprovalRequest) error {
	_, err := s.db.Exec(`
		INSERT INTO approval_requests (id, kingdom_id, trace_id, command, vassal_name, reason, status, responded_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.KingdomID, r.TraceID, r.Command, r.VassalName,
		nullableString(r.Reason), r.Status, nullableString(r.RespondedAt), r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert approval request: %w", err)
	}
	return nil
}

// GetApprovalRequest retrieves an approval request by ID.
func (s *Store) GetApprovalRequest(id string) (*ApprovalRequest, error) {
	row := s.db.QueryRow(`
		SELECT id, kingdom_id, trace_id, command, vassal_name, reason, status, responded_at, created_at
		FROM approval_requests WHERE id = ?`, id)
	return scanApprovalRequest(row)
}

// UpdateApprovalRequest updates the status and responded_at of an approval request.
func (s *Store) UpdateApprovalRequest(id, status, respondedAt string) error {
	res, err := s.db.Exec(`
		UPDATE approval_requests SET status = ?, responded_at = ? WHERE id = ?`,
		status, nullableString(respondedAt), id,
	)
	if err != nil {
		return fmt.Errorf("update approval request: %w", err)
	}
	return ensureRowAffected(res, "approval_request", id)
}

// GetApprovalRequestByTraceID retrieves the approval request linked to a given trace_id.
// Returns nil if none exists.
func (s *Store) GetApprovalRequestByTraceID(traceID string) (*ApprovalRequest, error) {
	row := s.db.QueryRow(`
		SELECT id, kingdom_id, trace_id, command, vassal_name, reason, status, responded_at, created_at
		FROM approval_requests WHERE trace_id = ?`, traceID)
	req, err := scanApprovalRequest(row)
	if err != nil {
		return nil, err
	}
	return req, nil
}

// ListPendingApprovals returns all pending approval requests for a kingdom.
func (s *Store) ListPendingApprovals(kingdomID string) ([]ApprovalRequest, error) {
	rows, err := s.db.Query(`
		SELECT id, kingdom_id, trace_id, command, vassal_name, reason, status, responded_at, created_at
		FROM approval_requests WHERE kingdom_id = ? AND status = 'pending' ORDER BY created_at DESC`, kingdomID)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals: %w", err)
	}
	defer rows.Close()

	var requests []ApprovalRequest
	for rows.Next() {
		r, err := scanApprovalRequestRow(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, *r)
	}
	return requests, rows.Err()
}

// ExpirePendingApprovals marks all pending approvals as "expired" for a kingdom.
func (s *Store) ExpirePendingApprovals(kingdomID string) error {
	_, err := s.db.Exec(`
		UPDATE approval_requests SET status = 'expired', responded_at = datetime('now')
		WHERE kingdom_id = ? AND status = 'pending'`, kingdomID)
	if err != nil {
		return fmt.Errorf("expire pending approvals: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scanner helpers
// ---------------------------------------------------------------------------

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanKingdom(s scanner) (*Kingdom, error) {
	var k Kingdom
	var pid sql.NullInt64
	if err := s.Scan(&k.ID, &k.Name, &k.RootPath, &k.SocketPath, &pid, &k.Status, &k.CreatedAt, &k.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan kingdom: %w", err)
	}
	if pid.Valid {
		k.PID = int(pid.Int64)
	}
	return &k, nil
}

func scanVassal(s scanner) (*Vassal, error) {
	v, err := scanVassalFromScanner(s)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func scanVassalFromScanner(s scanner) (*Vassal, error) {
	var v Vassal
	var pid sql.NullInt64
	var lastActivity sql.NullString
	if err := s.Scan(&v.ID, &v.KingdomID, &v.Name, &v.Command, &v.Status, &pid, &v.CreatedAt, &lastActivity); err != nil {
		return nil, fmt.Errorf("scan vassal: %w", err)
	}
	if pid.Valid {
		v.PID = int(pid.Int64)
	}
	if lastActivity.Valid {
		v.LastActivity = lastActivity.String
	}
	return &v, nil
}

func scanVassalRow(rows *sql.Rows) (*Vassal, error) {
	return scanVassalFromScanner(rows)
}

func scanArtifact(s scanner) (*Artifact, error) {
	a, err := scanArtifactFromScanner(s)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func scanArtifactFromScanner(s scanner) (*Artifact, error) {
	var a Artifact
	var producerID, mimeType, checksum sql.NullString
	if err := s.Scan(&a.ID, &a.KingdomID, &producerID, &a.Name, &a.FilePath, &mimeType, &a.Version, &checksum, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan artifact: %w", err)
	}
	if producerID.Valid {
		a.ProducerID = producerID.String
	}
	if mimeType.Valid {
		a.MimeType = mimeType.String
	}
	if checksum.Valid {
		a.Checksum = checksum.String
	}
	return &a, nil
}

func scanArtifactRow(rows *sql.Rows) (*Artifact, error) {
	return scanArtifactFromScanner(rows)
}

func scanEventRow(rows *sql.Rows) (*Event, error) {
	var e Event
	var sourceID, pattern, rawOutput, correlation sql.NullString
	var ack int
	if err := rows.Scan(&e.ID, &e.KingdomID, &sourceID, &e.Severity, &pattern, &e.Summary, &rawOutput, &correlation, &ack, &e.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan event: %w", err)
	}
	if sourceID.Valid {
		e.SourceID = sourceID.String
	}
	if pattern.Valid {
		e.Pattern = pattern.String
	}
	if rawOutput.Valid {
		e.RawOutput = rawOutput.String
	}
	if correlation.Valid {
		e.Correlation = correlation.String
	}
	e.Acknowledged = ack != 0
	return &e, nil
}

func scanAuditEntryRow(s scanner) (*AuditEntry, error) {
	var e AuditEntry
	var sourceID, traceID, metadata sql.NullString
	var sampled int
	if err := s.Scan(&e.ID, &e.KingdomID, &e.Layer, &e.Source, &sourceID,
		&e.Content, &traceID, &metadata, &sampled, &e.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan audit entry: %w", err)
	}
	if sourceID.Valid {
		e.SourceID = sourceID.String
	}
	if traceID.Valid {
		e.TraceID = traceID.String
	}
	if metadata.Valid {
		e.Metadata = metadata.String
	}
	e.Sampled = sampled != 0
	return &e, nil
}

func scanActionTrace(s scanner) (*ActionTrace, error) {
	t, err := scanActionTraceFromScanner(s)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func scanActionTraceFromScanner(s scanner) (*ActionTrace, error) {
	var t ActionTrace
	var vassalID, triggerEventID, output, completedAt sql.NullString
	var exitCode, durationMs sql.NullInt64
	if err := s.Scan(&t.TraceID, &t.KingdomID, &t.VassalName, &vassalID,
		&t.Command, &triggerEventID, &t.Status, &exitCode, &output,
		&durationMs, &t.StartedAt, &completedAt); err != nil {
		return nil, fmt.Errorf("scan action trace: %w", err)
	}
	if vassalID.Valid {
		t.VassalID = vassalID.String
	}
	if triggerEventID.Valid {
		t.TriggerEventID = triggerEventID.String
	}
	if exitCode.Valid {
		t.ExitCode = int(exitCode.Int64)
	}
	if output.Valid {
		t.Output = output.String
	}
	if durationMs.Valid {
		t.DurationMs = int(durationMs.Int64)
	}
	if completedAt.Valid {
		t.CompletedAt = completedAt.String
	}
	return &t, nil
}

func scanActionTraceRow(rows *sql.Rows) (*ActionTrace, error) {
	return scanActionTraceFromScanner(rows)
}

func scanApprovalRequest(s scanner) (*ApprovalRequest, error) {
	r, err := scanApprovalRequestFromScanner(s)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func scanApprovalRequestFromScanner(s scanner) (*ApprovalRequest, error) {
	var r ApprovalRequest
	var reason, respondedAt sql.NullString
	if err := s.Scan(&r.ID, &r.KingdomID, &r.TraceID, &r.Command,
		&r.VassalName, &reason, &r.Status, &respondedAt, &r.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan approval request: %w", err)
	}
	if reason.Valid {
		r.Reason = reason.String
	}
	if respondedAt.Valid {
		r.RespondedAt = respondedAt.String
	}
	return &r, nil
}

func scanApprovalRequestRow(rows *sql.Rows) (*ApprovalRequest, error) {
	return scanApprovalRequestFromScanner(rows)
}

// ---------------------------------------------------------------------------
// Nullable helpers
// ---------------------------------------------------------------------------

// nullableInt returns sql.NullInt64 that is NULL when v is 0.
func nullableInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

// nullableString returns sql.NullString that is NULL when v is empty.
func nullableString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

// ensureRowAffected returns an error when an UPDATE or DELETE touched zero rows.
func ensureRowAffected(res sql.Result, entity, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%s not found: %s", entity, id)
	}
	return nil
}
