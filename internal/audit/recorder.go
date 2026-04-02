package audit

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// AuditRecorder provides methods to record audit entries across
// the three layers: Ingestion, Sieve, and Action.
type AuditRecorder struct {
	store     *store.Store
	kingdomID string
	batcher   *BatchWriter
	logger    *slog.Logger
}

// NewAuditRecorder creates a new AuditRecorder.
func NewAuditRecorder(s *store.Store, kingdomID string, logger *slog.Logger) *AuditRecorder {
	return &AuditRecorder{
		store:     s,
		kingdomID: kingdomID,
		batcher:   NewBatchWriter(s, kingdomID, logger),
		logger:    logger,
	}
}

// Stop flushes and stops the internal BatchWriter.
func (r *AuditRecorder) Stop() {
	if r.batcher != nil {
		r.batcher.Stop()
	}
}

// RecordIngestion records a raw output line from a vassal (ingestion layer).
// Uses BatchWriter for buffered writes to reduce I/O overhead.
func (r *AuditRecorder) RecordIngestion(vassalName, vassalID, line string, sampled bool) {
	r.batcher.Add(vassalName, vassalID, line, sampled)
}

// SieveDecision holds metadata for a Sieve layer audit entry.
type SieveDecision struct {
	Decision string `json:"decision"` // "matched", "suppressed", "no_match"
	Pattern  string `json:"pattern,omitempty"`
	Severity string `json:"severity,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

// RecordSieve records a Sieve layer decision (match/suppress/no-match).
func (r *AuditRecorder) RecordSieve(vassalName, vassalID, content string, decision SieveDecision) {
	meta, _ := json.Marshal(decision)
	entry := store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: r.kingdomID,
		Layer:     "sieve",
		Source:    vassalName,
		SourceID:  vassalID,
		Content:   content,
		Metadata:  string(meta),
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	if err := r.store.CreateAuditEntry(entry); err != nil {
		r.logger.Warn("failed to record sieve audit", "error", err)
	}
}

// RecordAction records an action layer entry linked to a trace.
func (r *AuditRecorder) RecordAction(vassalName, vassalID, content, traceID string) {
	entry := store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: r.kingdomID,
		Layer:     "action",
		Source:    vassalName,
		SourceID:  vassalID,
		Content:   content,
		TraceID:   traceID,
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	if err := r.store.CreateAuditEntry(entry); err != nil {
		r.logger.Warn("failed to record action audit", "error", err)
	}
}

// Store returns the underlying store (for direct access by batcher).
func (r *AuditRecorder) Store() *store.Store {
	return r.store
}

// KingdomID returns the kingdom ID this recorder is bound to.
func (r *AuditRecorder) KingdomID() string {
	return r.kingdomID
}

// ParseRelativeTime converts a relative time string (e.g. "5m", "1h", "1d") or
// an RFC3339 string to a SQLite-compatible datetime string "YYYY-MM-DD HH:MM:SS".
// Returns empty string for empty input.
func ParseRelativeTime(s string) string {
	if s == "" {
		return ""
	}
	// Try relative: days suffix "1d", "7d".
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
			return time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour).Format("2006-01-02 15:04:05")
		}
	}
	// Try standard Go duration ("5m", "1h", "30s", "2h30m").
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().UTC().Add(-d).Format("2006-01-02 15:04:05")
	}
	// Try RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	// Try SQLite datetime format "2006-01-02 15:04:05".
	if _, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return s
	}
	// Try date-only "2006-01-02".
	if _, err := time.Parse("2006-01-02", s); err == nil {
		return s + " 00:00:00"
	}
	// Unrecognized format — return empty to avoid passing garbage to SQLite.
	return ""
}
