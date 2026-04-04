package store_test

import (
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newKingdom() store.Kingdom {
	return store.Kingdom{
		ID:         uuid.New().String(),
		Name:       "test-kingdom",
		RootPath:   "/tmp/test-kingdom",
		SocketPath: "/tmp/test.sock",
		Status:     "running",
		CreatedAt:  "2026-01-01 00:00:00",
		UpdatedAt:  "2026-01-01 00:00:00",
	}
}

// ---------------------------------------------------------------------------
// Kingdom
// ---------------------------------------------------------------------------

func TestKingdom_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()

	if err := s.CreateKingdom(k); err != nil {
		t.Fatalf("CreateKingdom: %v", err)
	}

	got, err := s.GetKingdom(k.ID)
	if err != nil {
		t.Fatalf("GetKingdom: %v", err)
	}
	if got.Name != k.Name || got.Status != k.Status || got.RootPath != k.RootPath {
		t.Errorf("got %+v, want %+v", got, k)
	}
}

func TestKingdom_GetByPath(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	got, err := s.GetKingdomByPath(k.RootPath)
	if err != nil {
		t.Fatalf("GetKingdomByPath: %v", err)
	}
	if got.ID != k.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, k.ID)
	}
}

func TestKingdom_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetKingdom("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing kingdom, got %+v", got)
	}
}

func TestKingdom_UpdateStatus(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	if err := s.UpdateKingdomStatus(k.ID, "stopped"); err != nil {
		t.Fatalf("UpdateKingdomStatus: %v", err)
	}
	got, _ := s.GetKingdom(k.ID)
	if got.Status != "stopped" {
		t.Errorf("expected status=stopped, got %s", got.Status)
	}
}

func TestKingdom_UpdatePID(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	if err := s.UpdateKingdomPID(k.ID, 12345); err != nil {
		t.Fatalf("UpdateKingdomPID: %v", err)
	}
	got, _ := s.GetKingdom(k.ID)
	if got.PID != 12345 {
		t.Errorf("expected PID=12345, got %d", got.PID)
	}
}

// ---------------------------------------------------------------------------
// Vassal
// ---------------------------------------------------------------------------

func newVassal(kingdomID string) store.Vassal {
	return store.Vassal{
		ID:        uuid.New().String(),
		KingdomID: kingdomID,
		Name:      "worker",
		Command:   "claude",
		Status:    "running",
		CreatedAt: "2026-01-01 00:00:00",
	}
}

func TestVassal_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	v := newVassal(k.ID)
	if err := s.CreateVassal(v); err != nil {
		t.Fatalf("CreateVassal: %v", err)
	}

	got, err := s.GetVassal(v.ID)
	if err != nil {
		t.Fatalf("GetVassal: %v", err)
	}
	if got.Name != v.Name || got.KingdomID != v.KingdomID {
		t.Errorf("got %+v, want %+v", got, v)
	}
}

func TestVassal_GetByName(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)
	v := newVassal(k.ID)
	_ = s.CreateVassal(v)

	got, err := s.GetVassalByName(k.ID, v.Name)
	if err != nil {
		t.Fatalf("GetVassalByName: %v", err)
	}
	if got.ID != v.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, v.ID)
	}
}

func TestVassal_ListByKingdom(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for i := 0; i < 3; i++ {
		v := newVassal(k.ID)
		v.Name = uuid.New().String() // unique names
		_ = s.CreateVassal(v)
	}

	vassals, err := s.ListVassals(k.ID)
	if err != nil {
		t.Fatalf("ListVassals: %v", err)
	}
	if len(vassals) != 3 {
		t.Errorf("expected 3 vassals, got %d", len(vassals))
	}
}

func TestVassal_UpdateStatus(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)
	v := newVassal(k.ID)
	_ = s.CreateVassal(v)

	if err := s.UpdateVassalStatus(v.ID, "stopped"); err != nil {
		t.Fatalf("UpdateVassalStatus: %v", err)
	}
	got, _ := s.GetVassal(v.ID)
	if got.Status != "stopped" {
		t.Errorf("expected status=stopped, got %s", got.Status)
	}
}

func TestVassal_Delete(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)
	v := newVassal(k.ID)
	_ = s.CreateVassal(v)

	if err := s.DeleteVassal(v.ID); err != nil {
		t.Fatalf("DeleteVassal: %v", err)
	}
	got, err := s.GetVassal(v.ID)
	if err != nil {
		t.Fatalf("unexpected error after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete, got non-nil vassal")
	}
}

// ---------------------------------------------------------------------------
// Event
// ---------------------------------------------------------------------------

func TestEvent_CreateAndList(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)
	v := newVassal(k.ID)
	_ = s.CreateVassal(v)

	ev := store.Event{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		SourceID:  v.ID,
		Severity:  "error",
		Pattern:   "panic_detected",
		Summary:   "panic: runtime error",
		RawOutput: "panic: runtime error: index out of range",
		CreatedAt: time.Now().UTC().Format(time.DateTime),
	}
	if err := s.CreateEvent(ev); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	events, err := s.ListEvents(k.ID, "", "", 10)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Summary != ev.Summary {
		t.Errorf("summary mismatch: got %q, want %q", events[0].Summary, ev.Summary)
	}
}

func TestEvent_FilterBySeverity(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)
	v := newVassal(k.ID)
	_ = s.CreateVassal(v)

	for _, sev := range []string{"error", "warn", "info"} {
		_ = s.CreateEvent(store.Event{
			ID:        uuid.New().String(),
			KingdomID: k.ID,
			SourceID:  v.ID,
			Severity:  sev,
			Pattern:   "p",
			CreatedAt: time.Now().UTC().Format(time.DateTime),
		})
	}

	errors, err := s.ListEvents(k.ID, "error", "", 10)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(errors) != 1 || errors[0].Severity != "error" {
		t.Errorf("expected 1 error event, got %+v", errors)
	}
}

// ---------------------------------------------------------------------------
// AuditEntry
// ---------------------------------------------------------------------------

func TestAuditEntry_Create(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	e := store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "sieve",
		Source:    "worker",
		SourceID:  "v1",
		Content:   "some output",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	if err := s.CreateAuditEntry(e); err != nil {
		t.Fatalf("CreateAuditEntry: %v", err)
	}
}

func TestAuditEntry_BatchWrite(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	batch := make([]store.AuditEntry, 5)
	for i := range batch {
		batch[i] = store.AuditEntry{
			ID:        uuid.New().String(),
			KingdomID: k.ID,
			Layer:     "ingestion",
			Source:    "worker",
			Content:   "line",
			CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
		}
	}
	if err := s.CreateAuditEntryBatch(batch); err != nil {
		t.Fatalf("CreateAuditEntryBatch: %v", err)
	}

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: k.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// DB accessor
// ---------------------------------------------------------------------------

func TestStore_DB(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()
	if db == nil {
		t.Fatal("DB() returned nil")
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("DB().Ping(): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Vassal — UpdateVassalPID
// ---------------------------------------------------------------------------

func TestVassal_UpdatePID(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)
	v := newVassal(k.ID)
	_ = s.CreateVassal(v)

	if err := s.UpdateVassalPID(v.ID, 9999); err != nil {
		t.Fatalf("UpdateVassalPID: %v", err)
	}
	got, _ := s.GetVassal(v.ID)
	if got.PID != 9999 {
		t.Errorf("expected PID=9999, got %d", got.PID)
	}
}

func TestVassal_UpdatePID_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateVassalPID("nonexistent", 1)
	if err == nil {
		t.Fatal("expected error for missing vassal, got nil")
	}
}

// ---------------------------------------------------------------------------
// Artifact CRUD
// ---------------------------------------------------------------------------

func newArtifact(kingdomID string) store.Artifact {
	return store.Artifact{
		ID:        uuid.New().String(),
		KingdomID: kingdomID,
		Name:      "output.txt",
		FilePath:  "/tmp/output.txt",
		MimeType:  "text/plain",
		Version:   1,
		Checksum:  "abc123",
		CreatedAt: "2026-01-01 00:00:00",
		UpdatedAt: "2026-01-01 00:00:00",
	}
}

func TestArtifact_CreateAndGetByName(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	a := newArtifact(k.ID)
	if err := s.CreateArtifact(a); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	got, err := s.GetArtifactByName(k.ID, a.Name)
	if err != nil {
		t.Fatalf("GetArtifactByName: %v", err)
	}
	if got == nil {
		t.Fatal("expected artifact, got nil")
	}
	if got.ID != a.ID || got.FilePath != a.FilePath {
		t.Errorf("got %+v, want %+v", got, a)
	}
}

func TestArtifact_GetByName_NotFound(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	got, err := s.GetArtifactByName(k.ID, "missing.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestArtifact_ListArtifacts(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for i := 0; i < 3; i++ {
		a := newArtifact(k.ID)
		a.ID = uuid.New().String()
		a.Name = uuid.New().String()
		_ = s.CreateArtifact(a)
	}

	artifacts, err := s.ListArtifacts(k.ID)
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(artifacts) != 3 {
		t.Errorf("expected 3 artifacts, got %d", len(artifacts))
	}
}

func TestArtifact_UpdateArtifact(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	a := newArtifact(k.ID)
	_ = s.CreateArtifact(a)

	a.FilePath = "/tmp/updated.txt"
	a.Version = 2
	a.Checksum = "def456"
	if err := s.UpdateArtifact(a); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}

	got, _ := s.GetArtifactByName(k.ID, a.Name)
	if got.FilePath != "/tmp/updated.txt" || got.Version != 2 || got.Checksum != "def456" {
		t.Errorf("unexpected artifact state: %+v", got)
	}
}

func TestArtifact_UpdateArtifact_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateArtifact(store.Artifact{ID: "nonexistent", Name: "x", FilePath: "/x", Version: 1})
	if err == nil {
		t.Fatal("expected error for missing artifact, got nil")
	}
}

// ---------------------------------------------------------------------------
// Event — AcknowledgeEvent, DeleteOldEvents
// ---------------------------------------------------------------------------

func TestEvent_AcknowledgeEvent(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	ev := store.Event{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Severity:  "error",
		Summary:   "test event",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	_ = s.CreateEvent(ev)

	if err := s.AcknowledgeEvent(ev.ID); err != nil {
		t.Fatalf("AcknowledgeEvent: %v", err)
	}

	events, _ := s.ListEvents(k.ID, "", "", 10)
	if len(events) != 1 || !events[0].Acknowledged {
		t.Errorf("expected acknowledged=true, got %+v", events)
	}
}

func TestEvent_AcknowledgeEvent_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.AcknowledgeEvent("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing event, got nil")
	}
}

func TestEvent_DeleteOldEvents(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	ev := store.Event{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Severity:  "info",
		Summary:   "old event",
		CreatedAt: "2020-01-01 00:00:00",
	}
	_ = s.CreateEvent(ev)

	ev2 := store.Event{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Severity:  "info",
		Summary:   "recent event",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	_ = s.CreateEvent(ev2)

	if err := s.DeleteOldEvents(k.ID, 1); err != nil {
		t.Fatalf("DeleteOldEvents: %v", err)
	}

	events, _ := s.ListEvents(k.ID, "", "", 10)
	if len(events) != 1 || events[0].Summary != "recent event" {
		t.Errorf("expected only recent event to remain, got %+v", events)
	}
}

// ---------------------------------------------------------------------------
// AuditEntry — CountAuditEntries, DeleteOldAuditEntries
// ---------------------------------------------------------------------------

func TestAuditEntry_CountAuditEntries(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for i := 0; i < 4; i++ {
		layer := "sieve"
		if i%2 == 0 {
			layer = "ingestion"
		}
		_ = s.CreateAuditEntry(store.AuditEntry{
			ID:        uuid.New().String(),
			KingdomID: k.ID,
			Layer:     layer,
			Source:    "test",
			Content:   "data",
			CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
		})
	}

	total, err := s.CountAuditEntries(store.AuditFilter{KingdomID: k.ID})
	if err != nil {
		t.Fatalf("CountAuditEntries: %v", err)
	}
	if total != 4 {
		t.Errorf("expected 4, got %d", total)
	}

	sieveCount, err := s.CountAuditEntries(store.AuditFilter{KingdomID: k.ID, Layer: "sieve"})
	if err != nil {
		t.Fatalf("CountAuditEntries sieve: %v", err)
	}
	if sieveCount != 2 {
		t.Errorf("expected 2 sieve entries, got %d", sieveCount)
	}
}

func TestAuditEntry_DeleteOldAuditEntries(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "ingestion",
		Source:    "test",
		Content:   "old ingestion",
		CreatedAt: "2020-01-01 00:00:00",
	})
	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "sieve",
		Source:    "test",
		Content:   "old sieve",
		CreatedAt: "2020-01-01 00:00:00",
	})
	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "action",
		Source:    "test",
		Content:   "recent",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})

	if err := s.DeleteOldAuditEntries(k.ID, 1, 1); err != nil {
		t.Fatalf("DeleteOldAuditEntries: %v", err)
	}

	count, _ := s.CountAuditEntries(store.AuditFilter{KingdomID: k.ID})
	if count != 1 {
		t.Errorf("expected 1 remaining entry, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// ActionTrace CRUD
// ---------------------------------------------------------------------------

func newActionTrace(kingdomID string) store.ActionTrace {
	return store.ActionTrace{
		TraceID:    uuid.New().String(),
		KingdomID:  kingdomID,
		VassalName: "worker",
		Command:    "echo hello",
		Status:     "running",
		StartedAt:  time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
}

func TestActionTrace_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	tr := newActionTrace(k.ID)
	if err := s.CreateActionTrace(tr); err != nil {
		t.Fatalf("CreateActionTrace: %v", err)
	}

	got, err := s.GetActionTrace(tr.TraceID)
	if err != nil {
		t.Fatalf("GetActionTrace: %v", err)
	}
	if got == nil {
		t.Fatal("expected trace, got nil")
	}
	if got.TraceID != tr.TraceID || got.Command != tr.Command {
		t.Errorf("got %+v, want %+v", got, tr)
	}
}

func TestActionTrace_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetActionTrace("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestActionTrace_Update(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	tr := newActionTrace(k.ID)
	_ = s.CreateActionTrace(tr)

	tr.Status = "completed"
	tr.ExitCode = 0
	tr.Output = "hello"
	tr.DurationMs = 42
	tr.CompletedAt = time.Now().UTC().Format("2006-01-02 15:04:05")

	if err := s.UpdateActionTrace(tr); err != nil {
		t.Fatalf("UpdateActionTrace: %v", err)
	}

	got, _ := s.GetActionTrace(tr.TraceID)
	if got.Status != "completed" || got.Output != "hello" || got.DurationMs != 42 {
		t.Errorf("unexpected trace state: %+v", got)
	}
}

func TestActionTrace_Update_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateActionTrace(store.ActionTrace{TraceID: "nonexistent", Status: "completed"})
	if err == nil {
		t.Fatal("expected error for missing trace, got nil")
	}
}

func TestActionTrace_List(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for i := 0; i < 3; i++ {
		tr := newActionTrace(k.ID)
		_ = s.CreateActionTrace(tr)
	}

	traces, err := s.ListActionTraces(k.ID, 10)
	if err != nil {
		t.Fatalf("ListActionTraces: %v", err)
	}
	if len(traces) != 3 {
		t.Errorf("expected 3 traces, got %d", len(traces))
	}
}

func TestActionTrace_List_ZeroLimit(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	traces, err := s.ListActionTraces(k.ID, 0)
	if err != nil {
		t.Fatalf("ListActionTraces with limit=0: %v", err)
	}
	_ = traces
}

// ---------------------------------------------------------------------------
// ApprovalRequest CRUD
// ---------------------------------------------------------------------------

func newApprovalRequest(kingdomID, traceID string) store.ApprovalRequest {
	return store.ApprovalRequest{
		ID:         uuid.New().String(),
		KingdomID:  kingdomID,
		TraceID:    traceID,
		Command:    "rm -rf /",
		VassalName: "worker",
		Reason:     "dangerous command",
		Status:     "pending",
		CreatedAt:  time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
}

func TestApprovalRequest_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	r := newApprovalRequest(k.ID, uuid.New().String())
	if err := s.CreateApprovalRequest(r); err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	got, err := s.GetApprovalRequest(r.ID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if got == nil {
		t.Fatal("expected approval request, got nil")
	}
	if got.ID != r.ID || got.Command != r.Command || got.Status != "pending" {
		t.Errorf("got %+v, want %+v", got, r)
	}
}

func TestApprovalRequest_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetApprovalRequest("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestApprovalRequest_Update(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	r := newApprovalRequest(k.ID, uuid.New().String())
	_ = s.CreateApprovalRequest(r)

	respondedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := s.UpdateApprovalRequest(r.ID, "approved", respondedAt); err != nil {
		t.Fatalf("UpdateApprovalRequest: %v", err)
	}

	got, _ := s.GetApprovalRequest(r.ID)
	if got.Status != "approved" || got.RespondedAt != respondedAt {
		t.Errorf("unexpected state: %+v", got)
	}
}

func TestApprovalRequest_Update_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateApprovalRequest("nonexistent", "approved", "")
	if err == nil {
		t.Fatal("expected error for missing request, got nil")
	}
}

func TestApprovalRequest_GetByTraceID(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	traceID := uuid.New().String()
	r := newApprovalRequest(k.ID, traceID)
	_ = s.CreateApprovalRequest(r)

	got, err := s.GetApprovalRequestByTraceID(traceID)
	if err != nil {
		t.Fatalf("GetApprovalRequestByTraceID: %v", err)
	}
	if got == nil || got.ID != r.ID {
		t.Errorf("expected request with ID=%s, got %+v", r.ID, got)
	}
}

func TestApprovalRequest_GetByTraceID_NotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetApprovalRequestByTraceID("nonexistent-trace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestApprovalRequest_ListPending(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for i := 0; i < 2; i++ {
		r := newApprovalRequest(k.ID, uuid.New().String())
		_ = s.CreateApprovalRequest(r)
	}
	approved := newApprovalRequest(k.ID, uuid.New().String())
	_ = s.CreateApprovalRequest(approved)
	_ = s.UpdateApprovalRequest(approved.ID, "approved", time.Now().UTC().Format("2006-01-02 15:04:05"))

	pending, err := s.ListPendingApprovals(k.ID)
	if err != nil {
		t.Fatalf("ListPendingApprovals: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending approvals, got %d", len(pending))
	}
}

// ---------------------------------------------------------------------------
// NewStore — invalid path
// ---------------------------------------------------------------------------

func TestNewStore_InvalidPath(t *testing.T) {
	// A path that can't be created should fail
	s, err := store.NewStore("/nonexistent/path/that/cannot/be/created/king.db")
	if err == nil {
		_ = s.Close()
		t.Fatal("expected error for invalid path, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListAuditEntries — filter coverage
// ---------------------------------------------------------------------------

func TestAuditEntry_ListWithSourceFilter(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for _, src := range []string{"worker-a", "worker-b", "worker-a"} {
		_ = s.CreateAuditEntry(store.AuditEntry{
			ID:        uuid.New().String(),
			KingdomID: k.ID,
			Layer:     "ingestion",
			Source:    src,
			Content:   "line",
			CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
		})
	}

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: k.ID, Source: "worker-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries with source filter: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries from worker-a, got %d", len(entries))
	}
}

func TestAuditEntry_ListWithTraceIDFilter(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	traceID := uuid.New().String()
	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "action",
		Source:    "worker",
		Content:   "with trace",
		TraceID:   traceID,
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "action",
		Source:    "worker",
		Content:   "no trace",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})

	entries, err := s.ListAuditEntries(store.AuditFilter{KingdomID: k.ID, TraceID: traceID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAuditEntries with traceID filter: %v", err)
	}
	if len(entries) != 1 || entries[0].TraceID != traceID {
		t.Errorf("expected 1 entry with traceID, got %+v", entries)
	}
}

func TestAuditEntry_ListWithSinceUntil(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "ingestion",
		Source:    "w",
		Content:   "old",
		CreatedAt: "2020-01-01 00:00:00",
	})
	_ = s.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: k.ID,
		Layer:     "ingestion",
		Source:    "w",
		Content:   "recent",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})

	entries, err := s.ListAuditEntries(store.AuditFilter{
		KingdomID: k.ID,
		Since:     "2025-01-01 00:00:00",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListAuditEntries with Since: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "recent" {
		t.Errorf("expected only recent entry, got %+v", entries)
	}
}

func TestAuditEntry_CountWithSourceFilter(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for _, src := range []string{"a", "a", "b"} {
		_ = s.CreateAuditEntry(store.AuditEntry{
			ID:        uuid.New().String(),
			KingdomID: k.ID,
			Layer:     "ingestion",
			Source:    src,
			Content:   "line",
			CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
		})
	}

	n, err := s.CountAuditEntries(store.AuditFilter{KingdomID: k.ID, Source: "a"})
	if err != nil {
		t.Fatalf("CountAuditEntries with source: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 entries from source 'a', got %d", n)
	}
}

func TestApprovalRequest_ExpirePending(t *testing.T) {
	s := newTestStore(t)
	k := newKingdom()
	_ = s.CreateKingdom(k)

	for i := 0; i < 3; i++ {
		r := newApprovalRequest(k.ID, uuid.New().String())
		_ = s.CreateApprovalRequest(r)
	}

	if err := s.ExpirePendingApprovals(k.ID); err != nil {
		t.Fatalf("ExpirePendingApprovals: %v", err)
	}

	pending, _ := s.ListPendingApprovals(k.ID)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after expire, got %d", len(pending))
	}
}
