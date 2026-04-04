package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newCoverageStore(t *testing.T) *Store {
	t.Helper()
	path := t.TempDir() + "/coverage_test.db"
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("newCoverageStore: %v", err)
	}
	return s
}

func seedKingdomCov(t *testing.T, s *Store) Kingdom {
	t.Helper()
	k := Kingdom{
		ID: uuid.New().String(), Name: "cov-kingdom",
		RootPath: "/tmp/cov-" + uuid.New().String(), SocketPath: "/tmp/cov.sock",
		Status: "running", CreatedAt: "2026-01-01 00:00:00", UpdatedAt: "2026-01-01 00:00:00",
	}
	if err := s.CreateKingdom(k); err != nil {
		t.Fatalf("seedKingdomCov: %v", err)
	}
	return k
}

func seedAuditEntryCov(t *testing.T, s *Store, kingdomID string) AuditEntry {
	t.Helper()
	e := AuditEntry{
		ID: uuid.New().String(), KingdomID: kingdomID, Layer: "ingestion",
		Source: "cov-source", Content: "cov content",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	if err := s.CreateAuditEntry(e); err != nil {
		t.Fatalf("seedAuditEntryCov: %v", err)
	}
	return e
}

func seedEventCov(t *testing.T, s *Store, kingdomID string) Event {
	t.Helper()
	e := Event{
		ID: uuid.New().String(), KingdomID: kingdomID, Severity: "info",
		Summary: "cov event", CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	if err := s.CreateEvent(e); err != nil {
		t.Fatalf("seedEventCov: %v", err)
	}
	return e
}

func TestCoverage_RunMigrations_ClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_ = db.Close()
	if err := RunMigrations(db); err == nil {
		t.Fatal("RunMigrations on closed DB: expected error, got nil")
	}
}

func TestCoverage_NewStore_InvalidPath(t *testing.T) {
	s, err := NewStore("/nonexistent_dir_abc/test.db")
	if err == nil {
		_ = s.Close()
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateKingdom_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	k := Kingdom{ID: uuid.New().String(), Name: "x", RootPath: "/tmp/x", SocketPath: "/tmp/x.sock", Status: "running", CreatedAt: "2026-01-01 00:00:00", UpdatedAt: "2026-01-01 00:00:00"}
	if err := s.CreateKingdom(k); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateKingdomStatus_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateKingdomStatus("any-id", "stopped"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateKingdomPID_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateKingdomPID("any-id", 1); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateKingdomStatus_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateKingdomStatus("nonexistent-id", "stopped"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateKingdomPID_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateKingdomPID("nonexistent-id", 1); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateVassalStatus_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateVassalStatus("any-id", "stopped"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateVassalPID_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateVassalPID("any-id", 1); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_DeleteVassal_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.DeleteVassal("any-id"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateVassalStatus_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateVassalStatus("nonexistent-id", "stopped"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateVassalPID_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateVassalPID("nonexistent-id", 1); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_DeleteVassal_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.DeleteVassal("nonexistent-id"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateArtifact_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	a := Artifact{ID: uuid.New().String(), KingdomID: "k1", Name: "art", FilePath: "/tmp/art.txt", Version: 1, CreatedAt: "2026-01-01 00:00:00", UpdatedAt: "2026-01-01 00:00:00"}
	if err := s.CreateArtifact(a); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateArtifact_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateArtifact(Artifact{ID: uuid.New().String(), Name: "art", FilePath: "/x", Version: 1}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateArtifact_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateArtifact(Artifact{ID: "nonexistent-id", Name: "x", FilePath: "/x", Version: 1}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateEvent_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	e := Event{ID: uuid.New().String(), KingdomID: "k1", Severity: "info", Summary: "test", CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05")}
	if err := s.CreateEvent(e); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_AcknowledgeEvent_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.AcknowledgeEvent("any-id"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_DeleteOldEvents_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.DeleteOldEvents("k1", 7); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_AcknowledgeEvent_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.AcknowledgeEvent("nonexistent-id"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateEvent_DuplicateID(t *testing.T) {
	s := newCoverageStore(t)
	defer s.db.Close()
	k := seedKingdomCov(t, s)
	ev := seedEventCov(t, s, k.ID)
	if err := s.CreateEvent(ev); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateAuditEntry_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	e := AuditEntry{ID: uuid.New().String(), KingdomID: "k1", Layer: "ingestion", Source: "src", Content: "data", CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05")}
	if err := s.CreateAuditEntry(e); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateAuditEntryBatch_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	batch := []AuditEntry{{ID: uuid.New().String(), KingdomID: "k1", Layer: "ingestion", Source: "src", Content: "data", CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05")}}
	if err := s.CreateAuditEntryBatch(batch); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateAuditEntryBatch_Empty(t *testing.T) {
	s := newCoverageStore(t)
	defer s.db.Close()
	if err := s.CreateAuditEntryBatch(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := s.CreateAuditEntryBatch([]AuditEntry{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoverage_CountAuditEntries_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if _, err := s.CountAuditEntries(AuditFilter{KingdomID: "k1"}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_DeleteOldAuditEntries_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.DeleteOldAuditEntries("k1", 7, 30); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateAuditEntry_DuplicateID(t *testing.T) {
	s := newCoverageStore(t)
	defer s.db.Close()
	k := seedKingdomCov(t, s)
	e := seedAuditEntryCov(t, s, k.ID)
	if err := s.CreateAuditEntry(e); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateAuditEntryBatch_DuplicateID(t *testing.T) {
	s := newCoverageStore(t)
	defer s.db.Close()
	k := seedKingdomCov(t, s)
	e := seedAuditEntryCov(t, s, k.ID)
	if err := s.CreateAuditEntryBatch([]AuditEntry{e}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CountAuditEntries_AllFilters(t *testing.T) {
	s := newCoverageStore(t)
	defer s.db.Close()
	k := seedKingdomCov(t, s)
	traceID := uuid.New().String()
	_ = s.CreateAuditEntry(AuditEntry{
		ID: uuid.New().String(), KingdomID: k.ID, Layer: "action", Source: "w",
		Content: "data", TraceID: traceID, CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	n, err := s.CountAuditEntries(AuditFilter{
		KingdomID: k.ID, Layer: "action", Source: "w",
		Since: "2020-01-01 00:00:00", Until: "2099-01-01 00:00:00", TraceID: traceID,
	})
	if err != nil {
		t.Fatalf("CountAuditEntries: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestCoverage_DeleteOldAuditEntries_BothExecs(t *testing.T) {
	s := newCoverageStore(t)
	defer s.db.Close()
	k := seedKingdomCov(t, s)
	_ = s.CreateAuditEntry(AuditEntry{ID: uuid.New().String(), KingdomID: k.ID, Layer: "ingestion", Source: "s", Content: "old", CreatedAt: "2020-01-01 00:00:00"})
	_ = s.CreateAuditEntry(AuditEntry{ID: uuid.New().String(), KingdomID: k.ID, Layer: "sieve", Source: "s", Content: "old", CreatedAt: "2020-01-01 00:00:00"})
	if err := s.DeleteOldAuditEntries(k.ID, 1, 1); err != nil {
		t.Fatalf("DeleteOldAuditEntries: %v", err)
	}
	count, _ := s.CountAuditEntries(AuditFilter{KingdomID: k.ID})
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCoverage_CreateActionTrace_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	tr := ActionTrace{TraceID: uuid.New().String(), KingdomID: "k1", VassalName: "w", Command: "echo", Status: "running", StartedAt: time.Now().UTC().Format("2006-01-02 15:04:05")}
	if err := s.CreateActionTrace(tr); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateActionTrace_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateActionTrace(ActionTrace{TraceID: uuid.New().String(), Status: "completed"}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateActionTrace_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateActionTrace(ActionTrace{TraceID: "nonexistent-id", Status: "completed"}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_CreateApprovalRequest_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	r := ApprovalRequest{ID: uuid.New().String(), KingdomID: "k1", TraceID: uuid.New().String(), Command: "ls", VassalName: "w", Status: "pending", CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05")}
	if err := s.CreateApprovalRequest(r); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateApprovalRequest_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.UpdateApprovalRequest("any-id", "approved", ""); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_ExpirePendingApprovals_ClosedDB(t *testing.T) {
	s := newCoverageStore(t)
	_ = s.db.Close()
	if err := s.ExpirePendingApprovals("k1"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCoverage_UpdateApprovalRequest_NotFound(t *testing.T) {
	s := newCoverageStore(t)
	defer s.Close()
	if err := s.UpdateApprovalRequest("nonexistent-id", "approved", ""); err == nil {
		t.Fatal("expected error, got nil")
	}
}
