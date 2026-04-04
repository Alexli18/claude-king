package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/store"
)

// ---------------------------------------------------------------------------
// fakeApprovalManager — satisfies ApprovalManager for tests
// ---------------------------------------------------------------------------

type fakeApprovalManager struct {
	respondErr error
}

func (f *fakeApprovalManager) Request(requestID string) <-chan bool {
	ch := make(chan bool, 1)
	return ch
}

func (f *fakeApprovalManager) Respond(requestID string, approved bool) error {
	return f.respondErr
}

func (f *fakeApprovalManager) Cancel(requestID string) bool {
	return true
}

// ---------------------------------------------------------------------------
// TestNewServer — constructor creates a non-nil server with correct fields
// ---------------------------------------------------------------------------

func TestNewServer_Basic(t *testing.T) {
	st := newTestStore(t)
	ledger := &fakeArtifactLedger{}
	ptyMgr := &fakePTYManager{sessions: map[string]*fakePTYSession{}}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := NewServer(ptyMgr, st, ledger, "test-kingdom-id", t.TempDir(), logger)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.kingdomID != "test-kingdom-id" {
		t.Errorf("expected kingdomID 'test-kingdom-id', got %q", srv.kingdomID)
	}
	if srv.store != st {
		t.Error("store not set correctly")
	}
	if srv.ledger != ledger {
		t.Error("ledger not set correctly")
	}
	if srv.ptyMgr != ptyMgr {
		t.Error("ptyMgr not set correctly")
	}
	if srv.mcpServer == nil {
		t.Error("mcpServer should not be nil")
	}
	if srv.activeHeartbeats == nil {
		t.Error("activeHeartbeats map should be initialised")
	}
}

func TestNewServer_NonNilLogger(t *testing.T) {
	st := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := NewServer(
		&fakePTYManager{sessions: map[string]*fakePTYSession{}},
		st,
		&fakeArtifactLedger{},
		"kid",
		t.TempDir(),
		logger,
	)
	if srv.logger == nil {
		t.Error("logger should not be nil after NewServer")
	}
}

// ---------------------------------------------------------------------------
// TestSetApprovalManager
// ---------------------------------------------------------------------------

func TestSetApprovalManager(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	if srv.approvalMgr != nil {
		t.Error("approvalMgr should be nil before SetApprovalManager")
	}

	mgr := &fakeApprovalManager{}
	srv.SetApprovalManager(mgr, true, 60)

	if srv.approvalMgr != mgr {
		t.Error("approvalMgr not set correctly")
	}
	if !srv.sovereignApproval {
		t.Error("sovereignApproval should be true")
	}
	if srv.sovereignApprovalTimeout != 60 {
		t.Errorf("expected timeout 60, got %d", srv.sovereignApprovalTimeout)
	}
}

func TestSetApprovalManager_Disabled(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	mgr := &fakeApprovalManager{}
	srv.SetApprovalManager(mgr, false, 0)

	if srv.sovereignApproval {
		t.Error("sovereignApproval should be false")
	}
}

// ---------------------------------------------------------------------------
// TestSetScanExecOutput
// ---------------------------------------------------------------------------

func TestSetScanExecOutput_Enable(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	if srv.scanExecOutput {
		t.Error("scanExecOutput should default to false")
	}
	srv.SetScanExecOutput(true)
	if !srv.scanExecOutput {
		t.Error("scanExecOutput should be true after SetScanExecOutput(true)")
	}
}

func TestSetScanExecOutput_Disable(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.SetScanExecOutput(true)
	srv.SetScanExecOutput(false)
	if srv.scanExecOutput {
		t.Error("scanExecOutput should be false after SetScanExecOutput(false)")
	}
}

// ---------------------------------------------------------------------------
// TestSetVassalPool
// ---------------------------------------------------------------------------

func TestSetVassalPool(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	if srv.vassalPool != nil {
		t.Error("vassalPool should be nil before SetVassalPool")
	}
	pool := &fakeVassalPool{vassals: map[string]*fakeVassalCaller{}}
	srv.SetVassalPool(pool)
	if srv.vassalPool != pool {
		t.Error("vassalPool not set correctly")
	}
}

func TestSetVassalPool_NilPool(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// should not panic
	srv.SetVassalPool(nil)
	if srv.vassalPool != nil {
		t.Error("vassalPool should be nil after setting nil")
	}
}

// ---------------------------------------------------------------------------
// TestSetVassalMeta / getVassalMeta
// ---------------------------------------------------------------------------

func TestSetVassalMeta(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	meta := map[string]VassalMeta{
		"coder":   {Type: "claude", Specialization: "coding"},
		"arduino": {Type: "serial", Specialization: ""},
	}
	srv.SetVassalMeta(meta)

	got, ok := srv.getVassalMeta("coder")
	if !ok {
		t.Fatal("expected to find 'coder' after SetVassalMeta")
	}
	if got.Type != "claude" {
		t.Errorf("expected type 'claude', got %q", got.Type)
	}
	if got.Specialization != "coding" {
		t.Errorf("expected specialization 'coding', got %q", got.Specialization)
	}
}

func TestSetVassalMeta_Unknown(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.SetVassalMeta(map[string]VassalMeta{})
	_, ok := srv.getVassalMeta("nonexistent")
	if ok {
		t.Error("expected not found for unknown vassal")
	}
}

func TestGetVassalMeta_NilMap(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// vassalMeta is nil by default
	_, ok := srv.getVassalMeta("anything")
	if ok {
		t.Error("expected false when vassalMeta is nil")
	}
}

// ---------------------------------------------------------------------------
// TestDetectParentKingdom — no registry file → no-op
// ---------------------------------------------------------------------------

func TestDetectParentKingdom_NoRegistry(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// Point to a non-existent registry path — should be a no-op (no panic, no state change)
	srv.detectParentKingdom(filepath.Join(t.TempDir(), "registry.json"))

	if srv.parentKingdomSocket != "" {
		t.Errorf("expected empty parentKingdomSocket, got %q", srv.parentKingdomSocket)
	}
	if srv.inObserverMode {
		t.Error("expected inObserverMode=false")
	}
}

func TestDetectParentKingdom_EmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	// Write an empty JSON object — no entries
	if err := os.WriteFile(regPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.rootDir = filepath.Join(dir, "child-kingdom")
	srv.detectParentKingdom(regPath)

	if srv.parentKingdomSocket != "" {
		t.Errorf("expected empty parentKingdomSocket, got %q", srv.parentKingdomSocket)
	}
	if srv.inObserverMode {
		t.Error("expected inObserverMode=false")
	}
}

// ---------------------------------------------------------------------------
// insertTestKingdom seeds the minimum required kingdom row so that
// action_traces and approval_requests FK constraints are satisfied.
// ---------------------------------------------------------------------------

func insertTestKingdom(t *testing.T, st *store.Store, kingdomID string) {
	t.Helper()
	err := st.CreateKingdom(store.Kingdom{
		ID:         kingdomID,
		Name:       "test-kingdom",
		RootPath:   "/tmp/test-kingdom-" + kingdomID,
		SocketPath: "/tmp/test-kingdom-" + kingdomID + ".sock",
		Status:     "running",
		CreatedAt:  "2026-01-01 00:00:00",
		UpdatedAt:  "2026-01-01 00:00:00",
	})
	if err != nil {
		t.Fatalf("insertTestKingdom: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestHandleGetActionTrace
// ---------------------------------------------------------------------------

func TestHandleGetActionTrace_MissingTraceID(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGetActionTrace(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when trace_id is missing")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "trace_id") {
		t.Errorf("expected 'trace_id' in error message, got: %s", text)
	}
}

func TestHandleGetActionTrace_NotFound(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGetActionTrace(context.Background(), makeRequest(map[string]any{
		"trace_id": "nonexistent-trace-id",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for non-existent trace_id")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "not found") {
		t.Errorf("expected 'not found' in error message, got: %s", text)
	}
}

func TestHandleGetActionTrace_Found(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})
	insertTestKingdom(t, st, "test-kingdom")

	// Insert a real ActionTrace into the store
	trace := store.ActionTrace{
		TraceID:    "trace-abc-123",
		KingdomID:  "test-kingdom",
		VassalName: "myvassal",
		Command:    "echo hello",
		Status:     "completed",
		ExitCode:   0,
		Output:     "hello",
		DurationMs: 42,
		StartedAt:  "2026-01-01 10:00:00",
	}
	if err := st.CreateActionTrace(trace); err != nil {
		t.Fatalf("CreateActionTrace: %v", err)
	}

	result, err := srv.handleGetActionTrace(context.Background(), makeRequest(map[string]any{
		"trace_id": "trace-abc-123",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(t, result))
	}

	text := extractText(t, result)
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["trace_id"] != "trace-abc-123" {
		t.Errorf("expected trace_id 'trace-abc-123', got %v", resp["trace_id"])
	}
	if resp["vassal_name"] != "myvassal" {
		t.Errorf("expected vassal_name 'myvassal', got %v", resp["vassal_name"])
	}
	if resp["command"] != "echo hello" {
		t.Errorf("expected command 'echo hello', got %v", resp["command"])
	}
}

func TestHandleGetActionTrace_WithApproval(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})
	insertTestKingdom(t, st, "test-kingdom")

	trace := store.ActionTrace{
		TraceID:    "trace-with-approval",
		KingdomID:  "test-kingdom",
		VassalName: "myvassal",
		Command:    "rm -rf /",
		Status:     "completed",
		StartedAt:  "2026-01-01 10:00:00",
	}
	if err := st.CreateActionTrace(trace); err != nil {
		t.Fatalf("CreateActionTrace: %v", err)
	}

	approval := store.ApprovalRequest{
		ID:         "approval-001",
		KingdomID:  "test-kingdom",
		TraceID:    "trace-with-approval",
		Command:    "rm -rf /",
		VassalName: "myvassal",
		Status:     "approved",
		CreatedAt:  "2026-01-01 10:00:00",
	}
	if err := st.CreateApprovalRequest(approval); err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	result, err := srv.handleGetActionTrace(context.Background(), makeRequest(map[string]any{
		"trace_id": "trace-with-approval",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(t, result))
	}

	text := extractText(t, result)
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["approval"] == nil {
		t.Error("expected 'approval' field in response when approval exists")
	}
}

// ---------------------------------------------------------------------------
// TestHandleRespondApproval
// ---------------------------------------------------------------------------

func TestHandleRespondApproval_SovereignApprovalDisabled(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// sovereignApproval defaults to false

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(map[string]any{
		"request_id": "some-id",
		"approved":   true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when sovereign_approval is disabled")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "not enabled") {
		t.Errorf("expected 'not enabled' in error, got: %s", text)
	}
}

func TestHandleRespondApproval_MissingRequestID(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.sovereignApproval = true

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when request_id is missing")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "request_id") {
		t.Errorf("expected 'request_id' in error message, got: %s", text)
	}
}

func TestHandleRespondApproval_NotFound(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.sovereignApproval = true

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(map[string]any{
		"request_id": "nonexistent-request",
		"approved":   true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent approval request")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "not found") {
		t.Errorf("expected 'not found' in error message, got: %s", text)
	}
}

func TestHandleRespondApproval_NotPending(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})
	srv.sovereignApproval = true
	insertTestKingdom(t, st, "test-kingdom")

	// Insert an already-approved request
	req := store.ApprovalRequest{
		ID:         "already-approved",
		KingdomID:  "test-kingdom",
		TraceID:    "some-trace",
		Command:    "echo hi",
		VassalName: "myvassal",
		Status:     "approved",
		CreatedAt:  "2026-01-01 10:00:00",
	}
	if err := st.CreateApprovalRequest(req); err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(map[string]any{
		"request_id": "already-approved",
		"approved":   false,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when request is not pending")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "not pending") {
		t.Errorf("expected 'not pending' in error message, got: %s", text)
	}
}

func TestHandleRespondApproval_Approve(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})
	srv.sovereignApproval = true
	srv.approvalMgr = &fakeApprovalManager{}
	insertTestKingdom(t, st, "test-kingdom")

	req := store.ApprovalRequest{
		ID:         "pending-request-001",
		KingdomID:  "test-kingdom",
		TraceID:    "trace-for-approval",
		Command:    "echo approve-me",
		VassalName: "myvassal",
		Status:     "pending",
		CreatedAt:  "2026-01-01 10:00:00",
	}
	if err := st.CreateApprovalRequest(req); err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(map[string]any{
		"request_id": "pending-request-001",
		"approved":   true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(t, result))
	}

	text := extractText(t, result)
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "approved" {
		t.Errorf("expected status 'approved', got %v", resp["status"])
	}
	if resp["request_id"] != "pending-request-001" {
		t.Errorf("expected request_id 'pending-request-001', got %v", resp["request_id"])
	}
}

func TestHandleRespondApproval_Reject(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})
	srv.sovereignApproval = true
	// No approvalMgr — exercises the nil-approvalMgr branch
	insertTestKingdom(t, st, "test-kingdom")

	req := store.ApprovalRequest{
		ID:         "pending-request-002",
		KingdomID:  "test-kingdom",
		TraceID:    "trace-for-reject",
		Command:    "rm important-file",
		VassalName: "myvassal",
		Status:     "pending",
		CreatedAt:  "2026-01-01 10:00:00",
	}
	if err := st.CreateApprovalRequest(req); err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(map[string]any{
		"request_id": "pending-request-002",
		"approved":   false,
		"reason":     "too dangerous",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(t, result))
	}

	text := extractText(t, result)
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "rejected" {
		t.Errorf("expected status 'rejected', got %v", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// register* helpers — verify they do not panic when called on a fresh server
// (NewServer already calls RegisterTools; calling individually exercises the
// function bodies for coverage without needing a full server start)
// ---------------------------------------------------------------------------

func TestRegisterDelegateControl_NoPanic(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("registerDelegateControl panicked: %v", r)
		}
	}()
	srv.registerDelegateControl()
}

func TestRegisterDelegateRelease_NoPanic(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("registerDelegateRelease panicked: %v", r)
		}
	}()
	srv.registerDelegateRelease()
}

func TestRegisterDelegateStatus_NoPanic(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("registerDelegateStatus panicked: %v", r)
		}
	}()
	srv.registerDelegateStatus()
}

// ---------------------------------------------------------------------------
// handleDelegateControl / Release / Status via handler — additional paths
// ---------------------------------------------------------------------------

func TestHandleDelegateControl_RegisteredNoPanic(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateControl(context.Background(), makeRequest(map[string]any{
		"vassal": "test-vassal",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error without parent kingdom socket")
	}
}

func TestHandleDelegateRelease_RegisteredNoPanic(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateRelease(context.Background(), makeRequest(map[string]any{
		"vassal": "test-vassal",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error without parent kingdom socket")
	}
}

func TestHandleDelegateStatus_RegisteredNoPanic(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateStatus(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(t, result))
	}
}

// ---------------------------------------------------------------------------
// handleDelegateControl / Release with a real fake daemon socket
// ---------------------------------------------------------------------------

func TestHandleDelegateControl_WithSocket(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"delegate_control": func(params json.RawMessage) (any, string) {
			return map[string]any{"delegated": true}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleDelegateControl(context.Background(), makeRequest(map[string]any{
		"vassal": "api",
		"force":  true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", extractText(t, result))
	}

	// Cancel the heartbeat goroutine to avoid leaks
	srv.heartbeatMu.Lock()
	if cancel, ok := srv.activeHeartbeats["api"]; ok {
		cancel()
		delete(srv.activeHeartbeats, "api")
	}
	srv.heartbeatMu.Unlock()
}

func TestHandleDelegateRelease_WithSocket(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"delegate_release": func(params json.RawMessage) (any, string) {
			return map[string]any{"released": true}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleDelegateRelease(context.Background(), makeRequest(map[string]any{
		"vassal": "api",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", extractText(t, result))
	}
}
