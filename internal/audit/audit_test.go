package audit_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/audit"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newMemStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedKingdom(t *testing.T, s *store.Store) string {
	t.Helper()
	id := uuid.New().String()
	_ = s.CreateKingdom(store.Kingdom{
		ID: id, Name: "k", RootPath: "/tmp/" + id,
		Status: "running", CreatedAt: "2026-01-01 00:00:00", UpdatedAt: "2026-01-01 00:00:00",
	})
	return id
}

// ---------------------------------------------------------------------------
// ApprovalManager
// ---------------------------------------------------------------------------

func TestApprovalManager_Approve(t *testing.T) {
	am := audit.NewApprovalManager()
	ch := am.Request("req-1")

	if err := am.Respond("req-1", true); err != nil {
		t.Fatalf("Respond: %v", err)
	}

	select {
	case approved := <-ch:
		if !approved {
			t.Error("expected approved=true")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval response")
	}
}

func TestApprovalManager_Deny(t *testing.T) {
	am := audit.NewApprovalManager()
	ch := am.Request("req-2")

	_ = am.Respond("req-2", false)

	select {
	case approved := <-ch:
		if approved {
			t.Error("expected approved=false")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for denial response")
	}
}

func TestApprovalManager_Respond_UnknownID(t *testing.T) {
	am := audit.NewApprovalManager()
	err := am.Respond("nonexistent", true)
	if err == nil {
		t.Fatal("expected error for unknown requestID, got nil")
	}
}

func TestApprovalManager_Cancel_Existing(t *testing.T) {
	am := audit.NewApprovalManager()
	am.Request("req-3")

	cancelled := am.Cancel("req-3")
	if !cancelled {
		t.Error("expected Cancel to return true for existing request")
	}

	// After cancel, Respond should return error.
	if err := am.Respond("req-3", true); err == nil {
		t.Error("expected error after cancel, got nil")
	}
}

func TestApprovalManager_Cancel_NonExistent(t *testing.T) {
	am := audit.NewApprovalManager()
	cancelled := am.Cancel("never-existed")
	if cancelled {
		t.Error("expected Cancel to return false for non-existent request")
	}
}

func TestApprovalManager_Concurrent(t *testing.T) {
	am := audit.NewApprovalManager()

	const n = 20
	channels := make([]<-chan bool, n)
	for i := 0; i < n; i++ {
		id := uuid.New().String()
		channels[i] = am.Request(id)
		go func(reqID string) {
			_ = am.Respond(reqID, true)
		}(id)
	}

	for i, ch := range channels {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for response %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// BatchWriter
// ---------------------------------------------------------------------------

func TestBatchWriter_FlushesOnStop(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)

	bw := audit.NewBatchWriter(s, kingdomID, discardLogger())

	const n = 5
	for i := 0; i < n; i++ {
		bw.Add("worker", "v1", "line content", false)
	}
	bw.Stop()

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Limit: 20})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != n {
		t.Errorf("expected %d entries after Stop, got %d", n, len(entries))
	}
}

func TestBatchWriter_StopIdempotent(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	bw := audit.NewBatchWriter(s, kingdomID, discardLogger())
	bw.Stop()
	bw.Stop() // should not panic
}

// ---------------------------------------------------------------------------
// ParseRelativeTime
// ---------------------------------------------------------------------------

func TestParseRelativeTime_Empty(t *testing.T) {
	if got := audit.ParseRelativeTime(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestParseRelativeTime_Days(t *testing.T) {
	got := audit.ParseRelativeTime("1d")
	if got == "" {
		t.Fatal("expected non-empty result for '1d'")
	}
}

func TestParseRelativeTime_Duration(t *testing.T) {
	got := audit.ParseRelativeTime("5m")
	if got == "" {
		t.Fatal("expected non-empty result for '5m'")
	}
}

func TestParseRelativeTime_RFC3339(t *testing.T) {
	got := audit.ParseRelativeTime("2026-01-15T10:00:00Z")
	if got == "" {
		t.Fatal("expected non-empty result for RFC3339")
	}
}

func TestParseRelativeTime_Invalid(t *testing.T) {
	got := audit.ParseRelativeTime("garbage-value")
	if got != "" {
		t.Errorf("expected empty for invalid input, got %q", got)
	}
}

func TestParseRelativeTime_SQLiteFormat(t *testing.T) {
	input := "2026-03-15 12:30:00"
	got := audit.ParseRelativeTime(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestParseRelativeTime_DateOnly(t *testing.T) {
	got := audit.ParseRelativeTime("2026-03-15")
	if got != "2026-03-15 00:00:00" {
		t.Errorf("expected '2026-03-15 00:00:00', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// AuditRecorder
// ---------------------------------------------------------------------------

func TestNewAuditRecorder_StoreAndKingdomID(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	r := audit.NewAuditRecorder(s, kingdomID, discardLogger())
	defer r.Stop()

	if r.Store() != s {
		t.Error("Store() should return the store passed to NewAuditRecorder")
	}
	if r.KingdomID() != kingdomID {
		t.Errorf("KingdomID() = %q, want %q", r.KingdomID(), kingdomID)
	}
}

func TestAuditRecorder_Stop_Idempotent(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	r := audit.NewAuditRecorder(s, kingdomID, discardLogger())
	r.Stop()
	r.Stop() // must not panic
}

func TestAuditRecorder_RecordIngestion(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	r := audit.NewAuditRecorder(s, kingdomID, discardLogger())

	r.RecordIngestion("worker", "v-id-1", "some log line", false)
	r.Stop()

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Layer: "ingestion", Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 ingestion entry, got %d", len(entries))
	}
	if entries[0].Source != "worker" {
		t.Errorf("expected source=worker, got %q", entries[0].Source)
	}
	if entries[0].Content != "some log line" {
		t.Errorf("expected content='some log line', got %q", entries[0].Content)
	}
}

func TestAuditRecorder_RecordSieve(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	r := audit.NewAuditRecorder(s, kingdomID, discardLogger())
	defer r.Stop()

	decision := audit.SieveDecision{
		Decision: "matched",
		Pattern:  "error.*",
		Severity: "high",
		Summary:  "error detected",
	}
	r.RecordSieve("worker", "v-id-1", "error: something went wrong", decision)

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Layer: "sieve", Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 sieve entry, got %d", len(entries))
	}
	if entries[0].Layer != "sieve" {
		t.Errorf("expected layer=sieve, got %q", entries[0].Layer)
	}
}

func TestAuditRecorder_RecordAction(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	r := audit.NewAuditRecorder(s, kingdomID, discardLogger())
	defer r.Stop()

	traceID := uuid.New().String()
	r.RecordAction("executor", "v-id-2", "ran command: ls", traceID)

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Layer: "action", Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 action entry, got %d", len(entries))
	}
	if entries[0].TraceID != traceID {
		t.Errorf("expected traceID=%q, got %q", traceID, entries[0].TraceID)
	}
}

// ---------------------------------------------------------------------------
// BatchWriter — additional coverage
// ---------------------------------------------------------------------------

func TestBatchWriter_Add_SampledFlag(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	bw := audit.NewBatchWriter(s, kingdomID, discardLogger())

	bw.Add("v", "id", "line", true)
	bw.Stop()

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].Sampled {
		t.Error("expected Sampled=true")
	}
}

func TestBatchWriter_SanitizesControlChars(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	bw := audit.NewBatchWriter(s, kingdomID, discardLogger())

	bw.Add("v", "id", "line\x01with\x02control", false)
	bw.Stop()

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Limit: 5})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content == "line\x01with\x02control" {
		t.Error("expected control characters to be sanitized")
	}
}

func TestBatchWriter_PreservesTab(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	bw := audit.NewBatchWriter(s, kingdomID, discardLogger())

	bw.Add("v", "id", "col1\tcol2", false)
	bw.Stop()

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Limit: 5})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "col1\tcol2" {
		t.Errorf("expected tab preserved, got %q", entries[0].Content)
	}
}

func TestBatchWriter_Add_FlushOnBatchSize(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	bw := audit.NewBatchWriter(s, kingdomID, discardLogger())

	for range 100 {
		bw.Add("v", "id", "line", false)
	}
	time.Sleep(100 * time.Millisecond)
	bw.Stop()

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: kingdomID, Limit: 200})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 100 {
		t.Errorf("expected 100 entries, got %d", len(entries))
	}
}
